#!/usr/bin/env bash
# AUDIT E1/E4 re-run of ONLY the CLAIM_OVERRIDE cells that failed the first pass
# (env-assignment-via-expansion bug). Appends to results/audit_readparity.jsonl.
#   A1_perrow_read       : per-row + payload read (claim_deliveries_read.sql), tiny payload
#   A3_log_noread        : claim-log count(*) index-only (claim_log_noread.sql), tiny payload
#   A1_perrow_read_sz1024/4096 : per-row + payload read, big payloads
# (A0/A2/A2_sz* already captured correctly in the first pass.)
set -uo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCALE="$(cd "$HERE/.." && pwd)"; source "$SCALE/env.sh"
RESULTS="$SCALE/results/audit_readparity.jsonl"
WARMUP="${WARMUP:-6}"; WINDOW="${WINDOW:-18}"; REPS="${REPS:-3}"
log(){ printf '[rp2 %s] %s\n' "$(date +%H:%M:%S)" "$*" >&2; }
zerop(){ psql_run "UPDATE progress SET n=0;"; }
vac(){ psql_run "VACUUM (ANALYZE) events;"; }
seed_ev(){ local n="$1" sz="$2" chunk=200000 c; local rem="$n"
  while (( rem > 0 )); do c="$chunk"; (( c > rem )) && c="$rem"
    if (( sz == 0 )); then psql_run "INSERT INTO events(payload) SELECT '{\"x\":1}'::jsonb FROM generate_series(1,$c);"
    else psql_run "INSERT INTO events(payload) SELECT jsonb_build_object('x', repeat('a', $sz)) FROM generate_series(1,$c);"; fi
    rem=$(( rem - c )); done; }
arm_dlv(){ psql_run "TRUNCATE deliveries;"; "$SCALE/seed_deliveries.sh" >/dev/null; zerop; }
arm_log(){ psql_run "UPDATE cursors SET claimed=0, committed=0;"; zerop; }

reps(){ # LABEL DESIGN CC BATCH NGROUPS OVERRIDE ARMFN [WIN]
  local label="$1" d="$2" cc="$3" b="$4" g="$5" ovr="$6" armfn="$7" win="${8:-$WINDOW}" i
  for i in $(seq 1 "$REPS"); do
    "$armfn"
    log "run ${label}_rep${i}"
    DESIGN="$d" RUN=consumer N_GROUPS="$g" CC="$cc" BATCH="$b" \
      WARMUP="$WARMUP" WINDOW="$win" LABEL="${label}_rep${i}" \
      CLAIM_OVERRIDE="$ovr" "$SCALE/measure.sh" 2>>"$SCALE/results/audit_rp.err" | tee -a "$RESULTS" || log "FAIL ${label}_rep${i}"
  done
}

psql_run "ALTER DATABASE \"$PGDATABASE\" SET synchronous_commit = on;"
log "START audit_readparity2 (override cells only)"

log "== A1 per-row+read tiny: d3 g10 500k events / 5M deliv =="
"$SCALE/reset.sh" d3 10 >/dev/null; seed_ev 500000 0; vac
reps "A1_perrow_read" d3 16 200 10 "claim_deliveries_read.sql" arm_dlv

log "== A3 claim-log no-read tiny: d4 g10 3M events =="
"$SCALE/reset.sh" d4 10 >/dev/null; seed_ev 3000000 0; vac
reps "A3_log_noread" d4 16 200 10 "claim_log_noread.sql" arm_log

for SZ in 1024 4096; do
  log "== A1 per-row+read ~${SZ}B: d3 g10 300k big events =="
  "$SCALE/reset.sh" d3 10 >/dev/null; seed_ev 300000 "$SZ"; vac
  reps "A1_perrow_read_sz${SZ}" d3 16 200 10 "claim_deliveries_read.sql" arm_dlv 12
done

log "DONE audit_readparity2"
