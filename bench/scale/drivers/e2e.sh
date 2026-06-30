#!/usr/bin/env bash
# Phase 4 driver: END-TO-END sustained throughput + BREAKING POINT + SPILL tier.
# Producer + consumer (+ projector for d2/d3) run concurrently per design.
#   (1) saturation: producer unthrottled -> sustained complete/s + who's the bottleneck
#   (2) breaking point: ramp producer offered RATE; find where backlog diverges
#   (3) spill: pre-grow the hot table to a tier and repeat saturation
# Emits JSON to results/e2e.jsonl. Detach in background (run DIRECTLY, not nohup&).
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCALE="$(cd "$HERE/.." && pwd)"; source "$SCALE/env.sh"
RESULTS="$SCALE/results/e2e.jsonl"
WARMUP="${WARMUP:-8}"; WINDOW="${WINDOW:-25}"
G="${G:-10}"
PC="${PC:-16}"; CC="${CC:-16}"; PBATCH="${PBATCH:-10}"; BATCH="${BATCH:-200}"
PROJ_N="${PROJ_N:-4}"; PROJ_BATCH="${PROJ_BATCH:-5000}"
DESIGNS="${DESIGNS:-d1 d2 d3 d4 d5}"
# breaking-point offered-rate ramp in EVENTS/s (measure.sh converts to pgbench -R
# via /PBATCH). Brackets both regimes: materialize designs diverge ~10-20k ev/s,
# no-fanout designs ~40-100k ev/s (at groups=10).
RAMP="${RAMP:-5000 10000 20000 40000 80000 160000}"

log(){ printf '[e2e %s] %s\n' "$(date +%H:%M:%S)" "$*" >&2; }
sync(){ psql_run "ALTER DATABASE \"$PGDATABASE\" SET synchronous_commit = $1;"; }
projflag(){ case "$1" in d2|d3) echo 1;; *) echo 0;; esac; }
projmode(){ case "$1" in d2) echo listen;; *) echo poll;; esac; }

# e2e_run LABEL DESIGN RATE
e2e_run(){
  local label="$1" d="$2" rate="$3"
  local pf pm; pf="$(projflag "$d")"; pm="$(projmode "$d")"
  log "run $label (d=$d rate=$rate proj=$pf/$pm)"
  DESIGN="$d" RUN=both N_GROUPS="$G" PC="$PC" CC="$CC" BATCH="$BATCH" PBATCH="$PBATCH" \
    RATE="$rate" PROJ="$pf" PROJ_MODE="$pm" PROJ_N="$PROJ_N" PROJ_BATCH="$PROJ_BATCH" \
    WARMUP="$WARMUP" WINDOW="$WINDOW" LABEL="$label" "$SCALE/measure.sh" | tee -a "$RESULTS"
}

: > "$RESULTS"
sync on
log "START e2e ladder warmup=$WARMUP window=$WINDOW groups=$G pc=$PC cc=$CC pbatch=$PBATCH"

for d in $DESIGNS; do
  ##### (1) saturation #####
  "$SCALE/reset.sh" "$d" "$G" >/dev/null
  e2e_run "sat_${d}_g${G}" "$d" 0

  ##### (2) breaking-point ramp (RAMP is events/s; pgbench -R = events/s / PBATCH) #####
  for ev in $RAMP; do
    txn=$(( ev / PBATCH )); (( txn < 1 )) && txn=1
    "$SCALE/reset.sh" "$d" "$G" >/dev/null
    e2e_run "ramp_${d}_g${G}_r${ev}" "$d" "$txn"
  done

  ##### (3) spill tier: pre-grow hot table to ~10M rows, then saturate #####
  "$SCALE/reset.sh" "$d" "$G" >/dev/null
  case "$d" in
    d1|d2|d3)  # pre-grow deliveries to ~10M (1M events x 10 groups), pre-materialized
      "$SCALE/seed_events.sh" 1000000 >/dev/null
      "$SCALE/seed_deliveries.sh" >/dev/null ;;
    d4|d5)     # pre-grow events log to 10M
      "$SCALE/seed_events.sh" 10000000 >/dev/null ;;
  esac
  e2e_run "spill_${d}_g${G}" "$d" 0
done

log "DONE e2e ladder -> $RESULTS"
