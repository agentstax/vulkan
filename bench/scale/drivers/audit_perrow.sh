#!/usr/bin/env bash
# AUDIT E3: try to break the per-row consume ~124k wall.
# Variants (g10, cc16, 3 reps, 5M ready backlog), all same-environment so deltas are clean:
#   base_b200     : claim_deliveries.sql (ready->inflight->acked, 2 UPDATEs/row)  [baseline]
#   base_b1000    : baseline, bigger batch
#   pop_b200      : claim_deliveries_popdelete.sql (SELECT FOR UPDATE SKIP LOCKED + DELETE, 1 txn)
#   pop_b1000     : pop-delete, bigger batch
#   unlog_base_b200 : baseline on an UNLOGGED deliveries table (removes WAL for the row writes)
#   unlog_pop_b200  : pop-delete on UNLOGGED deliveries
# -> results/audit_perrow.jsonl
set -uo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCALE="$(cd "$HERE/.." && pwd)"; source "$SCALE/env.sh"
RESULTS="$SCALE/results/audit_perrow.jsonl"
WARMUP="${WARMUP:-6}"; WINDOW="${WINDOW:-15}"; REPS="${REPS:-3}"
NEV="${NEV:-500000}"   # x g10 = 5M ready backlog

log(){ printf '[perrow %s] %s\n' "$(date +%H:%M:%S)" "$*" >&2; }
zerop(){ psql_run "UPDATE progress SET n=0;"; }
arm_dlv(){ psql_run "TRUNCATE deliveries;"; "$SCALE/seed_deliveries.sh" >/dev/null; zerop; }

reps(){ # LABEL CC BATCH OVERRIDE
  local label="$1" cc="$2" b="$3" ovr="$4" i
  for i in $(seq 1 "$REPS"); do
    arm_dlv
    log "run ${label}_rep${i} (cc=$cc b=$b ovr=${ovr:-none})"
    DESIGN=d3 RUN=consumer N_GROUPS=10 CC="$cc" BATCH="$b" \
      WARMUP="$WARMUP" WINDOW="$WINDOW" LABEL="${label}_rep${i}" \
      CLAIM_OVERRIDE="$ovr" "$SCALE/measure.sh" 2>>"$SCALE/results/audit_perrow.err" | tee -a "$RESULTS" || log "FAIL ${label}_rep${i}"
  done
}

psql_run "ALTER DATABASE \"$PGDATABASE\" SET synchronous_commit = on;"
: > "$RESULTS"; : > "$SCALE/results/audit_perrow.err"
log "START audit_perrow reps=$REPS window=$WINDOW backlog=$((NEV*10))"

############ LOGGED deliveries ############
log "== build d3 g10 (LOGGED), seed $NEV events =="
"$SCALE/reset.sh" d3 10 >/dev/null
"$SCALE/seed_events.sh" "$NEV" >/dev/null
reps "base_b200"  16 200  ""
reps "base_b1000" 16 1000 ""
reps "pop_b200"   16 200  "claim_deliveries_popdelete.sql"
reps "pop_b1000"  16 1000 "claim_deliveries_popdelete.sql"
# correctness for pop-delete: after a full drain, deliveries must be empty and done==seeded
log "== correctness: pop-delete exhaustive drain, assert deliveries==0 & done==backlog =="
arm_dlv
BL="$(psql_q "SELECT count(*) FROM deliveries;")"
zerop
DESIGN=d3 RUN=consumer N_GROUPS=10 CC=16 BATCH=200 CLAIM_OVERRIDE="claim_deliveries_popdelete.sql" \
  WARMUP=1 WINDOW=40 LABEL="pop_correctness_drain" "$SCALE/measure.sh" 2>>"$SCALE/results/audit_perrow.err" | tee -a "$RESULTS" || true
LEFT="$(psql_q "SELECT count(*) FROM deliveries;")"
DONEN="$(psql_q "SELECT COALESCE(sum(n),0) FROM progress;")"
log "CORRECTNESS pop-delete: backlog=$BL drained_done=$DONEN remaining=$LEFT"
if [[ "$LEFT" == "0" && "$DONEN" == "$BL" ]]; then log "CORRECTNESS pop-delete: PASS"; else log "CORRECTNESS pop-delete: *** CHECK ***"; fi

############ UNLOGGED deliveries ############
log "== rebuild d3 g10 with UNLOGGED deliveries =="
"$SCALE/reset.sh" d3 10 >/dev/null
psql_run "ALTER TABLE deliveries SET UNLOGGED;"
UNLOG="$(psql_q "SELECT relpersistence FROM pg_class WHERE relname='deliveries';")"  # expect 'u'
log "deliveries relpersistence=$UNLOG (u=unlogged)"
reps "unlog_base_b200" 16 200 ""
reps "unlog_pop_b200"  16 200 "claim_deliveries_popdelete.sql"

log "DONE audit_perrow -> $RESULTS"
