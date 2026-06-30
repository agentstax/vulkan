#!/usr/bin/env bash
# Phase 1 driver: PRODUCER-side ceilings.
#   (a) bare append (no fanout) — shared by d4/d5 and the producer side of d2/d3
#   (b) d1 synchronous statement-level trigger fanout tax across group counts
# Full OFAT optimization ladder: clients, batch, synchronous_commit, group
# commit (commit_delay), UNLOGGED ceiling. Emits one JSON line per run to
# results/producer.jsonl. Designed to run detached in the background.
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCALE="$(cd "$HERE/.." && pwd)"; source "$SCALE/env.sh"
RESULTS="$SCALE/results/producer.jsonl"
WARMUP="${WARMUP:-6}"; WINDOW="${WINDOW:-18}"
ANCHOR_PC="${ANCHOR_PC:-16}"; ANCHOR_PB="${ANCHOR_PB:-100}"

log(){ printf '[producer %s] %s\n' "$(date +%H:%M:%S)" "$*" >&2; }
sync(){ psql_run "ALTER DATABASE \"$PGDATABASE\" SET synchronous_commit = $1;"; }
guc(){ psql_run "ALTER SYSTEM SET $1 = $2;"; psql_run "SELECT pg_reload_conf();" >/dev/null; }
guc_reset(){ psql_run "ALTER SYSTEM RESET ALL;"; psql_run "SELECT pg_reload_conf();" >/dev/null; }
trunc(){ psql_run "TRUNCATE events RESTART IDENTITY CASCADE;"; }

# run LABEL DESIGN GROUPS PC PBATCH  -> producer-only measure, append JSON
run(){
  local label="$1" design="$2" ng="$3" pc="$4" pb="$5"
  log "run $label (design=$design groups=$ng pc=$pc pbatch=$pb)"
  DESIGN="$design" RUN=producer N_GROUPS="$ng" PC="$pc" PBATCH="$pb" \
    WARMUP="$WARMUP" WINDOW="$WINDOW" LABEL="$label" "$SCALE/measure.sh" | tee -a "$RESULTS"
}

: > "$RESULTS"
log "START producer ladder  warmup=$WARMUP window=$WINDOW anchor_pc=$ANCHOR_PC anchor_pb=$ANCHOR_PB"

############ (a) BARE APPEND (no trigger) ############
"$SCALE/reset.sh" d4 1 >/dev/null
sync on

log "== bare: client sweep (pbatch=1, sync=on) =="
for pc in 1 2 4 8 16 24 32 48 64; do trunc; run "bare_pc${pc}_b1_synON" d4 1 "$pc" 1; done

log "== bare: batch sweep (pc=$ANCHOR_PC, sync=on) =="
for pb in 1 10 100 1000; do trunc; run "bare_pc${ANCHOR_PC}_b${pb}_synON" d4 1 "$ANCHOR_PC" "$pb"; done

log "== bare: synchronous_commit=off =="
sync off
trunc; run "bare_pc${ANCHOR_PC}_b1_synOFF"   d4 1 "$ANCHOR_PC" 1
trunc; run "bare_pc${ANCHOR_PC}_b${ANCHOR_PB}_synOFF" d4 1 "$ANCHOR_PC" "$ANCHOR_PB"
sync on

log "== bare: group commit (commit_delay sweep, sync=on, pbatch=1) =="
for cd in 50 100 200; do
  guc commit_delay "$cd"; guc commit_siblings 5
  trunc; run "bare_pc${ANCHOR_PC}_b1_synON_cd${cd}" d4 1 "$ANCHOR_PC" 1
done
guc_reset

log "== bare: UNLOGGED events ceiling =="
psql_run "DROP TABLE IF EXISTS events CASCADE; CREATE UNLOGGED TABLE events (\"offset\" BIGSERIAL PRIMARY KEY, payload JSONB NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT now());"
sync on;  trunc; run "bare_pc${ANCHOR_PC}_b${ANCHOR_PB}_UNLOGGED_synON"  d4 1 "$ANCHOR_PC" "$ANCHOR_PB"
sync off; trunc; run "bare_pc${ANCHOR_PC}_b${ANCHOR_PB}_UNLOGGED_synOFF" d4 1 "$ANCHOR_PC" "$ANCHOR_PB"
sync on

############ (b) d1 SYNCHRONOUS TRIGGER FANOUT TAX ############
log "== d1: fanout tax across groups (pc=$ANCHOR_PC, pbatch=1, sync=on) =="
for ng in 1 10 50 100 500; do
  "$SCALE/reset.sh" d1 "$ng" >/dev/null
  run "d1_fanout_g${ng}_pc${ANCHOR_PC}_b1_synON" d1 "$ng" "$ANCHOR_PC" 1
  # verify fanout: deliveries_added should equal events_added * ng (checked in analysis via JSON)
done

log "== d1: fanout tax sync=off (groups 10,100,500) =="
sync off
for ng in 10 100 500; do
  "$SCALE/reset.sh" d1 "$ng" >/dev/null
  run "d1_fanout_g${ng}_pc${ANCHOR_PC}_b1_synOFF" d1 "$ng" "$ANCHOR_PC" 1
done
sync on

log "== d1: fanout tax with batched insert (pbatch=$ANCHOR_PB, groups 10,100) =="
for ng in 10 100; do
  "$SCALE/reset.sh" d1 "$ng" >/dev/null
  run "d1_fanout_g${ng}_pc${ANCHOR_PC}_b${ANCHOR_PB}_synON" d1 "$ng" "$ANCHOR_PC" "$ANCHOR_PB"
done

guc_reset; sync on
log "DONE producer ladder -> $RESULTS"
