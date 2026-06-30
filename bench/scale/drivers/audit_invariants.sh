#!/usr/bin/env bash
# AUDIT E6: ENFORCE the correctness invariants the report only asserted in prose.
# Reviewer B (rightly) flagged that no script actually checks done==events*groups, no
# dup/no-skip, leases-drained. This driver runs small EXHAUSTIVE concurrent drains and
# asserts the terminal identities, exiting nonzero on any violation.
#   d4 claim-from-log : sum(progress.n)==head*G AND every cursor committed==claimed==head
#   d5 range-lease    : same AND leases table empty (0 dangling leases)
#   d3 per-row        : acked==head*G AND sum(progress.n)==head*G AND 0 rows left 'ready'/'inflight'
# -> results/audit_invariants.txt (human log); exit 0 = all PASS
set -uo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCALE="$(cd "$HERE/.." && pwd)"; source "$SCALE/env.sh"
OUT="$SCALE/results/audit_invariants.txt"
G="${G:-10}"; NEV="${NEV:-100000}"; CC="${CC:-32}"   # 32 concurrent clients => stress dup/skip
log(){ printf '[inv %s] %s\n' "$(date +%H:%M:%S)" "$*" | tee -a "$OUT" >&2; }
fail=0

: > "$OUT"
psql_run "ALTER DATABASE \"$PGDATABASE\" SET synchronous_commit = on;"
log "START invariants G=$G events=$NEV cc=$CC"

drain_to_zero(){ # DESIGN OVERRIDE  -- drain until progress stops growing
  local d="$1" ovr="$2" last=-1 cur
  for _ in $(seq 1 30); do
    DESIGN="$d" RUN=consumer N_GROUPS="$G" CC="$CC" BATCH=200 \
      CLAIM_OVERRIDE="$ovr" WARMUP=0 WINDOW=4 LABEL="inv_${d}" \
      "$SCALE/measure.sh" >/dev/null 2>>"$SCALE/results/audit_inv.err" || true
    cur="$(psql_q "SELECT COALESCE(sum(n),0) FROM progress;")"
    [[ "$cur" == "$last" ]] && break
    last="$cur"
  done
}

############ d4 claim-from-log ############
log "== d4 claim-from-log =="
"$SCALE/reset.sh" d4 "$G" >/dev/null
"$SCALE/seed_events.sh" "$NEV" >/dev/null
HEAD="$(psql_q "SELECT max(\"offset\") FROM events;")"
psql_run "UPDATE progress SET n=0;"
drain_to_zero d4 ""
DONEN="$(psql_q "SELECT COALESCE(sum(n),0) FROM progress;")"
BADCUR="$(psql_q "SELECT count(*) FROM cursors WHERE committed<>$HEAD OR claimed<>$HEAD;")"
EXP=$(( HEAD * G ))
log "d4: head=$HEAD G=$G expected_units=$EXP done=$DONEN cursors_not_at_head=$BADCUR"
if [[ "$DONEN" == "$EXP" && "$BADCUR" == "0" ]]; then log "d4: PASS (done==head*G, all cursors at head => no dup/no skip)"; else log "d4: *** FAIL ***"; fail=1; fi

############ d5 range-lease ############
log "== d5 range-lease =="
"$SCALE/reset.sh" d5 "$G" >/dev/null
"$SCALE/seed_events.sh" "$NEV" >/dev/null
HEAD="$(psql_q "SELECT max(\"offset\") FROM events;")"
psql_run "UPDATE progress SET n=0;"
drain_to_zero d5 ""
DONEN="$(psql_q "SELECT COALESCE(sum(n),0) FROM progress;")"
BADCUR="$(psql_q "SELECT count(*) FROM cursors WHERE committed<>$HEAD OR claimed<>$HEAD;")"
LEASES="$(psql_q "SELECT count(*) FROM leases;")"
EXP=$(( HEAD * G ))
log "d5: head=$HEAD expected_units=$EXP done=$DONEN cursors_not_at_head=$BADCUR open_leases=$LEASES"
if [[ "$DONEN" == "$EXP" && "$BADCUR" == "0" && "$LEASES" == "0" ]]; then log "d5: PASS (done==head*G, cursors at head, 0 dangling leases)"; else log "d5: *** FAIL ***"; fail=1; fi

############ d3 per-row deliveries ############
log "== d3 per-row deliveries =="
"$SCALE/reset.sh" d3 "$G" >/dev/null
"$SCALE/seed_events.sh" "$NEV" >/dev/null
"$SCALE/seed_deliveries.sh" >/dev/null
HEAD="$(psql_q "SELECT max(\"offset\") FROM events;")"
BL="$(psql_q "SELECT count(*) FROM deliveries;")"
psql_run "UPDATE progress SET n=0;"
drain_to_zero d3 ""
DONEN="$(psql_q "SELECT COALESCE(sum(n),0) FROM progress;")"
ACKED="$(psql_q "SELECT count(*) FROM deliveries WHERE state='acked';")"
NOTDONE="$(psql_q "SELECT count(*) FROM deliveries WHERE state IN ('ready','inflight');")"
EXP=$(( HEAD * G ))
log "d3: head=$HEAD backlog=$BL expected=$EXP done=$DONEN acked=$ACKED ready_or_inflight_left=$NOTDONE"
if [[ "$DONEN" == "$EXP" && "$ACKED" == "$EXP" && "$NOTDONE" == "0" ]]; then log "d3: PASS (every delivery acked exactly once)"; else log "d3: *** FAIL ***"; fail=1; fi

log "DONE invariants (fail=$fail)"
exit "$fail"
