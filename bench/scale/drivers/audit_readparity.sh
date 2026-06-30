#!/usr/bin/env bash
# AUDIT E1 + E4: read-parity matrix and payload-size sensitivity.
# Answers Goal-1a: is the matched per-row vs claim-from-log comparison apples-to-apples,
# and how does the ratio move when (i) the per-row consumer ALSO reads the payload and
# (ii) payloads get large?
#
# Matrix (g10, b200, cc16, median-of-REPS), tiny {"x":..} payload:
#   A0 perrow_noread  : claim_deliveries.sql        (2 writes/row, NO event read)   [current baseline]
#   A1 perrow_read    : claim_deliveries_read.sql   (2 writes/row + heap read)      [fair]
#   A2 log_read       : claim_log.sql               (cursor advance + count(payload) heap read) [current]
#   A3 log_noread     : claim_log_noread.sql        (cursor advance + count(*) index-only)
# Then E4: A1 vs A2 at payload ~1KB and ~4KB.
# -> results/audit_readparity.jsonl
set -uo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCALE="$(cd "$HERE/.." && pwd)"; source "$SCALE/env.sh"
RESULTS="$SCALE/results/audit_readparity.jsonl"
WARMUP="${WARMUP:-6}"; WINDOW="${WINDOW:-18}"; REPS="${REPS:-3}"

log(){ printf '[rp %s] %s\n' "$(date +%H:%M:%S)" "$*" >&2; }
zerop(){ psql_run "UPDATE progress SET n=0;"; }
vac(){ psql_run "VACUUM (ANALYZE) events;"; }

# seed N events with payload of ~SZ bytes (SZ=0 -> tiny {"x":1})
seed_ev(){
  local n="$1" sz="$2" chunk=200000 c
  local rem="$n"
  while (( rem > 0 )); do
    c="$chunk"; (( c > rem )) && c="$rem"
    if (( sz == 0 )); then
      psql_run "INSERT INTO events(payload) SELECT '{\"x\":1}'::jsonb FROM generate_series(1,$c);"
    else
      psql_run "INSERT INTO events(payload) SELECT jsonb_build_object('x', repeat('a', $sz)) FROM generate_series(1,$c);"
    fi
    rem=$(( rem - c ))
  done
}

# reps LABEL DESIGN CC BATCH NGROUPS CLAIM_OVERRIDE ARMFN [WINDOW]
reps(){
  local label="$1" d="$2" cc="$3" b="$4" g="$5" ovr="$6" armfn="$7" win="${8:-$WINDOW}" i
  for i in $(seq 1 "$REPS"); do
    "$armfn"
    log "run ${label}_rep${i} (d=$d g=$g cc=$cc b=$b ovr=${ovr:-none} win=$win)"
    DESIGN="$d" RUN=consumer N_GROUPS="$g" CC="$cc" BATCH="$b" \
      WARMUP="$WARMUP" WINDOW="$win" LABEL="${label}_rep${i}" \
      CLAIM_OVERRIDE="$ovr" "$SCALE/measure.sh" 2>>"$SCALE/results/audit_rp.err" | tee -a "$RESULTS" || log "RUN FAILED ${label}_rep${i}"
  done
}

: > "$RESULTS"; : > "$SCALE/results/audit_rp.err"
psql_run "ALTER DATABASE \"$PGDATABASE\" SET synchronous_commit = on;"
log "START audit_readparity reps=$REPS warmup=$WARMUP window=$WINDOW"

############################################################
# E1 — tiny payload, full 4-cell matrix
############################################################
# --- per-row (A0, A1): 5M ready backlog = 500k events x g10 ---
log "== E1 per-row: build d3 g10, 500k events, 5M deliveries =="
"$SCALE/reset.sh" d3 10 >/dev/null
seed_ev 500000 0
vac
arm_dlv(){ psql_run "TRUNCATE deliveries;"; "$SCALE/seed_deliveries.sh" >/dev/null; zerop; }
reps "A0_perrow_noread" d3 16 200 10 ""                              arm_dlv
reps "A1_perrow_read"   d3 16 200 10 "claim_deliveries_read.sql"     arm_dlv

# --- claim-log (A2, A3): 30M backlog = 3M events x g10 ---
log "== E1 claim-log: build d4 g10, 3M events =="
"$SCALE/reset.sh" d4 10 >/dev/null
seed_ev 3000000 0
vac
arm_log(){ psql_run "UPDATE cursors SET claimed=0, committed=0;"; zerop; }
reps "A2_log_read"   d4 16 200 10 ""                        arm_log
reps "A3_log_noread" d4 16 200 10 "claim_log_noread.sql"    arm_log

############################################################
# E4 — payload-size sensitivity: A1 (per-row+read) vs A2 (log+read)
############################################################
for SZ in 1024 4096; do
  log "== E4 payload ~${SZ}B: per-row A1 =="
  "$SCALE/reset.sh" d3 10 >/dev/null
  seed_ev 300000 "$SZ"     # 3M deliveries, big events
  vac
  reps "A1_perrow_read_sz${SZ}" d3 16 200 10 "claim_deliveries_read.sql" arm_dlv 12

  log "== E4 payload ~${SZ}B: claim-log A2 =="
  "$SCALE/reset.sh" d4 10 >/dev/null
  seed_ev 1500000 "$SZ"    # big events; claim-log consumes more so seed more
  vac
  reps "A2_log_read_sz${SZ}" d4 16 200 10 "" arm_log 12
done

log "DONE audit_readparity -> $RESULTS"
