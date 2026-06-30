#!/usr/bin/env bash
# Phase 3 driver: CONSUMER-side DRAIN ceilings (no producer; pre-seeded backlog).
#   A) deliveries claim+ack  (shared by d1/d2/d3)
#   B) claim-from-log        (d4) — sweeps groups to expose single-frontier contention
#   C) range-lease           (d5)
# Ladder: clients, batch, synchronous_commit, split- vs combined-commit.
# Emits JSON lines to results/drain.jsonl. Detach in background.
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCALE="$(cd "$HERE/.." && pwd)"; source "$SCALE/env.sh"
RESULTS="$SCALE/results/drain.jsonl"
WARMUP="${WARMUP:-6}"; WINDOW="${WINDOW:-20}"

log(){ printf '[drain %s] %s\n' "$(date +%H:%M:%S)" "$*" >&2; }
sync(){ psql_run "ALTER DATABASE \"$PGDATABASE\" SET synchronous_commit = $1;"; }

# runc LABEL DESIGN GROUPS CC BATCH [CLAIM_OVERRIDE]
runc(){
  local label="$1" d="$2" g="$3" cc="$4" b="$5" ov="${6:-}"
  log "run $label (d=$d g=$g cc=$cc batch=$b ov=${ov:-none})"
  CLAIM_OVERRIDE="$ov" DESIGN="$d" RUN=consumer N_GROUPS="$g" CC="$cc" BATCH="$b" \
    WARMUP="$WARMUP" WINDOW="$WINDOW" LABEL="$label" "$SCALE/measure.sh" | tee -a "$RESULTS"
}

: > "$RESULTS"
log "START drain ladder  warmup=$WARMUP window=$WINDOW"

############ A) deliveries claim+ack drain (d1/d2/d3) ############
GA=10; EVA="${EVA:-700000}"   # 700k events x 10 groups = 7M ready deliveries
log "== A setup: d3 g$GA, seed $EVA events =="
"$SCALE/reset.sh" d3 "$GA" >/dev/null
"$SCALE/seed_events.sh" "$EVA" >/dev/null
arm_dlv(){ psql_run "TRUNCATE deliveries;"; "$SCALE/seed_deliveries.sh" >/dev/null; }

sync on
for cc in 8 16 32 48; do arm_dlv; runc "dlv_cc${cc}_b200_synON" d3 "$GA" "$cc" 200; done
for b in 50 1000; do arm_dlv; runc "dlv_cc32_b${b}_synON" d3 "$GA" 32 "$b"; done
arm_dlv; runc "dlv_cc32_b200_combined_synON" d3 "$GA" 32 200 claim_deliveries_combined.sql
sync off
arm_dlv; runc "dlv_cc32_b200_synOFF"          d3 "$GA" 32 200
arm_dlv; runc "dlv_cc32_b200_combined_synOFF" d3 "$GA" 32 200 claim_deliveries_combined.sql
sync on

############ B) claim-from-log drain (d4) ############
EVB="${EVB:-5000000}"
arm_log(){ psql_run "UPDATE cursors SET claimed=0, committed=0;"; }
for g in 1 10 100; do
  log "== B setup: d4 g$g, seed $EVB events =="
  "$SCALE/reset.sh" d4 "$g" >/dev/null
  "$SCALE/seed_events.sh" "$EVB" >/dev/null
  sync on
  for cc in 8 16 32; do arm_log; runc "log_g${g}_cc${cc}_b200_synON" d4 "$g" "$cc" 200; done
  arm_log; runc "log_g${g}_cc32_b1000_synON" d4 "$g" 32 1000
  sync off
  arm_log; runc "log_g${g}_cc32_b200_synOFF" d4 "$g" 32 200
  sync on
done

############ C) range-lease drain (d5) ############
log "== C setup: d5 g10, seed $EVB events =="
"$SCALE/reset.sh" d5 10 >/dev/null
"$SCALE/seed_events.sh" "$EVB" >/dev/null
arm_lease(){ psql_run "UPDATE cursors SET claimed=0, committed=0; TRUNCATE leases;"; }
sync on
for cc in 8 16 32; do arm_lease; runc "lease_g10_cc${cc}_b200_synON" d5 10 "$cc" 200; done
arm_lease; runc "lease_g10_cc32_b1000_synON" d5 10 32 1000
sync off
arm_lease; runc "lease_g10_cc32_b200_synOFF" d5 10 32 200
sync on

log "DONE drain ladder -> $RESULTS"
