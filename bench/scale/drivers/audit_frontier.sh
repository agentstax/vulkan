#!/usr/bin/env bash
# AUDIT E2: shard the claim-from-log frontier to lift the g1 single-frontier wall.
# g1 (one group) is where all clients contend on ONE cursor row (~100-128k units/s).
# We split group g0 into K disjoint contiguous offset blocks, each with its own cursor
# row 'g0#s' (claim_log_sharded.sql). Sweeping K should relax the contention and raise
# the wall toward the per-event heap-read / core ceiling. Also gives the REPLICATED
# (3-rep) g1 number that the audit (dimension F) flagged as N=1 + order-of-magnitude noisy.
# -> results/audit_frontier.jsonl
set -uo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCALE="$(cd "$HERE/.." && pwd)"; source "$SCALE/env.sh"
RESULTS="$SCALE/results/audit_frontier.jsonl"
WARMUP="${WARMUP:-6}"; WINDOW="${WINDOW:-18}"; REPS="${REPS:-3}"
NEV="${NEV:-16000000}"   # 16M events => 16M units backlog for g1 (non-exhausting even at K=16)

log(){ printf '[front %s] %s\n' "$(date +%H:%M:%S)" "$*" >&2; }

psql_run "ALTER DATABASE \"$PGDATABASE\" SET synchronous_commit = on;"
: > "$RESULTS"; : > "$SCALE/results/audit_front.err"
log "START audit_frontier reps=$REPS window=$WINDOW NEV=$NEV"

log "== build d4 g1, seed $NEV events =="
"$SCALE/reset.sh" d4 1 >/dev/null
"$SCALE/seed_events.sh" "$NEV" >/dev/null
psql_run "VACUUM (ANALYZE) events;"
HEAD="$(psql_q "SELECT COALESCE(max(\"offset\"),0) FROM events;")"
log "head=$HEAD"

# (re)build K disjoint sharded cursor rows for group g0 and reset progress
arm_shard(){ # $1=K
  local k="$1"
  psql_run "
    DELETE FROM cursors;
    INSERT INTO cursors(consumer_group, claimed, committed, projected)
    SELECT 'g0#'||s,
           ($HEAD::bigint * s / $k),
           ($HEAD::bigint * s / $k),
           CASE WHEN s = $k - 1 THEN $HEAD ELSE ($HEAD::bigint * (s+1) / $k) END
    FROM generate_series(0, $k - 1) s;
    UPDATE progress SET n=0;"
}

run_k(){ # $1=K $2=CC
  local k="$1" cc="$2" i
  for i in $(seq 1 "$REPS"); do
    arm_shard "$k"
    log "run kfront=$k cc=$cc rep$i"
    DESIGN=d4 RUN=consumer N_GROUPS=1 CC="$cc" BATCH=200 KFRONT="$k" \
      CLAIM_OVERRIDE="claim_log_sharded.sql" \
      WARMUP="$WARMUP" WINDOW="$WINDOW" LABEL="front_k${k}_cc${cc}_rep${i}" \
      "$SCALE/measure.sh" 2>>"$SCALE/results/audit_front.err" | tee -a "$RESULTS" || log "FAIL k$k cc$cc rep$i"
  done
}

for K in 1 2 4 8 16; do
  run_k "$K" 16
done
# also the highest shard count at cc32 to see if more clients now help
run_k 16 32

# correctness on the sharded path: drain g0 K=8 to completion on a small head, assert exact
log "== correctness: exhaustive sharded drain, assert done==head, no gap/overlap =="
"$SCALE/reset.sh" d4 1 >/dev/null
"$SCALE/seed_events.sh" 200000 >/dev/null
SMALLHEAD="$(psql_q "SELECT max(\"offset\") FROM events;")"
HEAD="$SMALLHEAD"; arm_shard 8
# drain hard until backlog ~0
DESIGN=d4 RUN=consumer N_GROUPS=1 CC=16 BATCH=200 KFRONT=8 \
  CLAIM_OVERRIDE="claim_log_sharded.sql" WARMUP=1 WINDOW=20 LABEL="front_correctness_drain" \
  "$SCALE/measure.sh" 2>>"$SCALE/results/audit_front.err" | tee -a "$RESULTS" || true
DONEN="$(psql_q "SELECT COALESCE(sum(n),0) FROM progress;")"
SUMCLAIMED="$(psql_q "SELECT COALESCE(sum(claimed),0) - COALESCE(sum(CASE WHEN consumer_group LIKE 'g0#%' THEN 0 END),0) FROM cursors;")"
# each shard advanced claimed from base..cap; total offsets consumed = sum(claimed) - sum(base).
# base for shard s = head*s/8. sum(base over s=0..7) :
SUMBASE="$(psql_q "SELECT COALESCE(sum($SMALLHEAD::bigint * s / 8),0) FROM generate_series(0,7) s;")"
CONSUMED=$(( SUMCLAIMED - SUMBASE ))
ALLCAP="$(psql_q "SELECT bool_and(claimed = projected) FROM cursors WHERE consumer_group LIKE 'g0#%';")"
log "CORRECTNESS sharded: head=$SMALLHEAD done=$DONEN consumed_offsets=$CONSUMED all_blocks_full=$ALLCAP"
if [[ "$DONEN" == "$SMALLHEAD" && "$CONSUMED" == "$SMALLHEAD" && "$ALLCAP" == "t" ]]; then
  log "CORRECTNESS sharded: PASS (done==head==consumed, every block fully drained, disjoint => no dup/gap)"
else
  log "CORRECTNESS sharded: *** CHECK *** done=$DONEN head=$SMALLHEAD consumed=$CONSUMED full=$ALLCAP"
fi

log "DONE audit_frontier -> $RESULTS"
