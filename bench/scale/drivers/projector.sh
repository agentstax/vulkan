#!/usr/bin/env bash
# Phase 2 driver: MATERIALIZATION via projector (designs d2/d3).
#   (a) materialize ceiling on a static backlog: offset-window batch sweep, then
#       parallel-shard sweep (poll mode).
#   (b) keep-up with a LIVE producer: poll vs listen (NOTIFY), report projector
#       lag (head - projected).
#   (c) materialize cost at higher fanout width (groups=100).
# Emits JSON lines to results/projector.jsonl. Detach in background.
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCALE="$(cd "$HERE/.." && pwd)"; source "$SCALE/env.sh"
RESULTS="$SCALE/results/projector.jsonl"
WARMUP="${WARMUP:-6}"; WINDOW="${WINDOW:-20}"

log(){ printf '[proj %s] %s\n' "$(date +%H:%M:%S)" "$*" >&2; }
sync(){ psql_run "ALTER DATABASE \"$PGDATABASE\" SET synchronous_commit = $1;"; }

# runp LABEL GROUPS MODE NSHARDS BATCH RUNKIND PC PBATCH
#   RUNKIND=ceiling -> projector-only on static backlog (RUN=none)
#   RUNKIND=keepup  -> live producer + projector (RUN=producer)
runp(){
  local label="$1" g="$2" mode="$3" n="$4" b="$5" kind="$6" pc="${7:-0}" pb="${8:-1}"
  local runmode=none
  [[ "$kind" == "keepup" ]] && runmode=producer
  log "run $label (g=$g mode=$mode shards=$n batch=$b kind=$kind pc=$pc pbatch=$pb)"
  DESIGN=d2 RUN="$runmode" PROJ=1 PROJ_MODE="$mode" PROJ_N="$n" PROJ_BATCH="$b" \
    N_GROUPS="$g" PC="$pc" PBATCH="$pb" WARMUP="$WARMUP" WINDOW="$WINDOW" \
    LABEL="$label" "$SCALE/measure.sh" | tee -a "$RESULTS"
}
# record projector lag after a keep-up run
keepup_lag(){
  local label="$1"
  local H MP mp
  H="$(psql_q "SELECT COALESCE(max(\"offset\"),0) FROM events;")"
  MP="$(psql_q "SELECT COALESCE(max(projected),0) FROM cursors;")"
  mp="$(psql_q "SELECT COALESCE(min(projected),0) FROM cursors;")"
  printf '{"label":"%s_lag","head":%s,"max_projected":%s,"min_projected":%s,"lag":%s}\n' \
    "$label" "$H" "$MP" "$mp" "$(( H - mp ))" | tee -a "$RESULTS"
}

: > "$RESULTS"
sync on
log "START projector ladder warmup=$WARMUP window=$WINDOW"

############ (a) materialize ceiling — static backlog, groups=10 ############
GP=10; EVP=$(( 20000000 / GP ))   # ~20M deliveries worth of events
log "== setup d2 g$GP, seed $EVP events =="
"$SCALE/reset.sh" d2 "$GP" >/dev/null
"$SCALE/seed_events.sh" "$EVP" >/dev/null
arm(){ psql_run "TRUNCATE deliveries; UPDATE cursors SET projected=0;"; psql_run "CHECKPOINT;"; }

log "== poll: offset-window batch sweep (1 shard) =="
for b in 1000 5000 20000 50000; do arm; runp "mat_poll_n1_b${b}" "$GP" poll 1 "$b" ceiling; done

log "== poll: parallel-shard sweep (batch=20000) =="
for n in 1 2 4 8; do arm; runp "mat_poll_n${n}_b20000" "$GP" poll "$n" 20000 ceiling; done

############ (b) keep-up with live producer: poll vs listen ############
log "== keep-up: live producer (pc16 pbatch10) + projector (4 shards) =="
for mode in poll listen; do
  "$SCALE/reset.sh" d2 "$GP" >/dev/null
  runp "keep_${mode}_n4_b5000" "$GP" "$mode" 4 5000 keepup 16 10
  keepup_lag "keep_${mode}_n4_b5000"
done

############ (c) materialize cost at higher fanout width (groups=100) ############
GP2=100; EVP2=$(( 20000000 / GP2 ))
log "== setup d2 g$GP2, seed $EVP2 events =="
"$SCALE/reset.sh" d2 "$GP2" >/dev/null
"$SCALE/seed_events.sh" "$EVP2" >/dev/null
arm2(){ psql_run "TRUNCATE deliveries; UPDATE cursors SET projected=0;"; psql_run "CHECKPOINT;"; }
for n in 1 4 8; do arm2; runp "mat_g100_poll_n${n}_b20000" "$GP2" poll "$n" 20000 ceiling; done

log "DONE projector ladder -> $RESULTS"
