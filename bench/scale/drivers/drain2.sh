#!/usr/bin/env bash
# Phase 3b driver: CORRECTED + REPLICATED drain ceilings, addressing the audit.
#   H1: claim_log/rangelease now count(payload) -> per-event HEAP read (real work).
#   H2: non-exhausting backlogs; gate on backlog_end>0 (now valid since H-fix below).
#   H3: re-run dlv b1000 to back/replace the "141k" claim.
#   H4: 3 reps per config -> median-of-medians + run-to-run spread.
#   backlog diagnostics: reset progress per run so head*groups-done is per-run valid.
# Produces clean MATCHED d3/d4/d5 triples at g10,b200,cc{16,32}. -> results/drain2.jsonl
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCALE="$(cd "$HERE/.." && pwd)"; source "$SCALE/env.sh"
RESULTS="$SCALE/results/drain2.jsonl"
WARMUP="${WARMUP:-6}"; WINDOW="${WINDOW:-24}"; REPS="${REPS:-3}"

log(){ printf '[drain2 %s] %s\n' "$(date +%H:%M:%S)" "$*" >&2; }
sync(){ psql_run "ALTER DATABASE \"$PGDATABASE\" SET synchronous_commit = $1;"; }
zerop(){ psql_run "UPDATE progress SET n=0;"; }

# reps LABEL DESIGN GROUPS CC BATCH ARMFN
reps(){
  local label="$1" d="$2" g="$3" cc="$4" b="$5" armfn="$6" i
  for i in $(seq 1 "$REPS"); do
    "$armfn"
    log "run ${label}_rep${i} (d=$d g=$g cc=$cc batch=$b)"
    DESIGN="$d" RUN=consumer N_GROUPS="$g" CC="$cc" BATCH="$b" \
      WARMUP="$WARMUP" WINDOW="$WINDOW" LABEL="${label}_rep${i}" "$SCALE/measure.sh" | tee -a "$RESULTS"
  done
}

: > "$RESULTS"; sync on
log "START drain2 (corrected+replicated) reps=$REPS window=$WINDOW"

############ A) per-row deliveries (d3) g10 — 7M backlog ############
log "== A: deliveries d3 g10, seed 700k events =="
"$SCALE/reset.sh" d3 10 >/dev/null
"$SCALE/seed_events.sh" 700000 >/dev/null
arm_dlv(){ psql_run "TRUNCATE deliveries;"; "$SCALE/seed_deliveries.sh" >/dev/null; zerop; }
reps "dlv_g10_cc16_b200"  d3 10 16 200  arm_dlv
reps "dlv_g10_cc32_b200"  d3 10 32 200  arm_dlv
reps "dlv_g10_cc32_b1000" d3 10 32 1000 arm_dlv   # H3: back/replace the b1000 number

############ B) claim-from-log (d4) HEAP-READ, non-exhausting ############
log "== B: claim-log d4 g10, seed 3M events (30M units) =="
"$SCALE/reset.sh" d4 10 >/dev/null
"$SCALE/seed_events.sh" 3000000 >/dev/null
arm_log(){ psql_run "UPDATE cursors SET claimed=0, committed=0;"; zerop; }
reps "log_g10_cc16_b200" d4 10 16 200 arm_log
reps "log_g10_cc32_b200" d4 10 32 200 arm_log

log "== B: claim-log d4 g100, seed 5M events (500M units) =="
"$SCALE/reset.sh" d4 100 >/dev/null
"$SCALE/seed_events.sh" 5000000 >/dev/null
reps "log_g100_cc32_b200" d4 100 32 200 arm_log

############ C) range-lease (d5) HEAP-READ, non-exhausting ############
log "== C: range-lease d5 g10, seed 3M events =="
"$SCALE/reset.sh" d5 10 >/dev/null
"$SCALE/seed_events.sh" 3000000 >/dev/null
arm_lease(){ psql_run "UPDATE cursors SET claimed=0, committed=0; TRUNCATE leases;"; zerop; }
reps "lease_g10_cc16_b200" d5 10 16 200 arm_lease
reps "lease_g10_cc32_b200" d5 10 32 200 arm_lease

log "DONE drain2 -> $RESULTS"
