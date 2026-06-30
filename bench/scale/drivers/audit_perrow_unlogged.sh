#!/usr/bin/env bash
# AUDIT E3 fix: UNLOGGED deliveries variants (the first pass measured 0 because the
# unlogged section re-ran reset.sh which drops/recreates events EMPTY and never
# re-seeded the log, so seed_deliveries cross-joined nothing). Here we seed events
# THEN make deliveries UNLOGGED. Appends to results/audit_perrow.jsonl.
set -uo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCALE="$(cd "$HERE/.." && pwd)"; source "$SCALE/env.sh"
RESULTS="$SCALE/results/audit_perrow.jsonl"
WARMUP="${WARMUP:-6}"; WINDOW="${WINDOW:-15}"; REPS="${REPS:-3}"; NEV="${NEV:-500000}"
log(){ printf '[unlog %s] %s\n' "$(date +%H:%M:%S)" "$*" >&2; }
arm_dlv(){ psql_run "TRUNCATE deliveries;"; "$SCALE/seed_deliveries.sh" >/dev/null; psql_run "UPDATE progress SET n=0;"; }
reps(){ local label="$1" b="$2" ovr="$3" i
  for i in $(seq 1 "$REPS"); do arm_dlv; log "run ${label}_rep${i}"
    DESIGN=d3 RUN=consumer N_GROUPS=10 CC=16 BATCH="$b" WARMUP="$WARMUP" WINDOW="$WINDOW" \
      LABEL="${label}_rep${i}" CLAIM_OVERRIDE="$ovr" "$SCALE/measure.sh" \
      2>>"$SCALE/results/audit_perrow.err" | tee -a "$RESULTS" || log "FAIL ${label}_rep${i}"; done; }

psql_run "ALTER DATABASE \"$PGDATABASE\" SET synchronous_commit = on;"
log "== rebuild d3 g10, seed $NEV events, deliveries UNLOGGED =="
"$SCALE/reset.sh" d3 10 >/dev/null
"$SCALE/seed_events.sh" "$NEV" >/dev/null
psql_run "ALTER TABLE deliveries SET UNLOGGED;"
log "deliveries relpersistence=$(psql_q "SELECT relpersistence FROM pg_class WHERE relname='deliveries';") (u=unlogged)"
reps "unlog_base_b200" 200  ""
reps "unlog_pop_b200"  200  "claim_deliveries_popdelete.sql"
reps "unlog_pop_b1000" 1000 "claim_deliveries_popdelete.sql"
log "DONE unlogged"
