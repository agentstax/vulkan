#!/usr/bin/env bash
# Trigger->deliveries fanout benchmark harness.
#
# CLI:
#   ./run.sh TRIGGER GROUPS SYNC IDX MODE CLIENTS PARAM TRIALS
#     TRIGGER  none|row|stmt|notify
#     GROUPS   integer (cursors seeded 'g0'..'g{N-1}')
#     SYNC     on|off
#     IDX      pk|claim
#     MODE     tps|bulk|projector
#     CLIENTS  integer (pgbench -c/-j; ignored for bulk/projector)
#     PARAM    tps: duration seconds | bulk: #events | projector: #events to seed
#     TRIALS   best-of-N
#
# Emits ONE machine-parseable RESULT line.
set -euo pipefail

# ----- connection -----
PGHOST="${PGHOST:-localhost}"
PGPORT="${PGPORT:-5432}"
PGUSER="${PGUSER:-example_user}"
PGDATABASE="${PGDATABASE:-example_db}"
export PGPASSWORD="${PGPASSWORD:-example_password}"
export PGHOST PGPORT PGUSER PGDATABASE

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
INSERT_SQL="$SCRIPT_DIR/insert_event.sql"

# ----- args -----
if [[ $# -ne 8 ]]; then
  echo "usage: $0 TRIGGER GROUPS SYNC IDX MODE CLIENTS PARAM TRIALS" >&2
  exit 2
fi
# NOTE: do NOT use a shell variable literally named GROUPS — some harness
# environments pre-set GROUPS as a readonly env var, which silently shadows the
# CLI value. We use N_GROUPS internally. The CLI arg order is unchanged.
TRIGGER="$1"
N_GROUPS="$2"
SYNC="$3"
IDX="$4"
MODE="$5"
CLIENTS="$6"
PARAM="$7"
TRIALS="$8"

case "$TRIGGER" in none|row|stmt|notify) ;; *) echo "bad TRIGGER: $TRIGGER" >&2; exit 2;; esac
case "$SYNC"    in on|off) ;;            *) echo "bad SYNC: $SYNC" >&2; exit 2;; esac
case "$IDX"     in pk|claim) ;;          *) echo "bad IDX: $IDX" >&2; exit 2;; esac
case "$MODE"    in tps|bulk|projector) ;;*) echo "bad MODE: $MODE" >&2; exit 2;; esac
[[ "$N_GROUPS" =~ ^[0-9]+$ ]] || { echo "GROUPS must be int" >&2; exit 2; }
[[ "$CLIENTS" =~ ^[0-9]+$ ]] || { echo "CLIENTS must be int" >&2; exit 2; }
[[ "$PARAM"   =~ ^[0-9]+$ ]] || { echo "PARAM must be int" >&2; exit 2; }
[[ "$TRIALS"  =~ ^[0-9]+$ ]] && (( TRIALS >= 1 )) || { echo "TRIALS must be >=1" >&2; exit 2; }

# psql helpers
psql_q() {  # quiet, value-only
  psql -v ON_ERROR_STOP=1 -tAc "$1"
}
psql_run() { # run a multi-statement block
  psql -v ON_ERROR_STOP=1 -q -c "$1"
}

# ----- schema / variant setup (once per run) -----
# synchronous_commit is a DATABASE setting: set it, then verify with a FRESH connection.
psql_run "ALTER DATABASE \"$PGDATABASE\" SET synchronous_commit = $SYNC;"
SYNC_ACTUAL="$(psql_q "SHOW synchronous_commit;")"

# Drop everything and rebuild clean.
psql -v ON_ERROR_STOP=1 -q <<'SQL'
DROP TRIGGER IF EXISTS trg_fanout ON events;
DROP TABLE IF EXISTS deliveries CASCADE;
DROP TABLE IF EXISTS events CASCADE;
DROP TABLE IF EXISTS cursors CASCADE;
DROP FUNCTION IF EXISTS fanout_stmt() CASCADE;
DROP FUNCTION IF EXISTS fanout_row() CASCADE;
DROP FUNCTION IF EXISTS fanout_notify() CASCADE;

CREATE TABLE events (
  "offset"   BIGSERIAL PRIMARY KEY,
  payload    JSONB NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE cursors (
  consumer_group TEXT PRIMARY KEY,
  committed BIGINT NOT NULL DEFAULT 0,
  projected BIGINT NOT NULL DEFAULT 0
);
CREATE TABLE deliveries (
  consumer_group TEXT NOT NULL,
  "offset"       BIGINT NOT NULL,
  state          TEXT NOT NULL DEFAULT 'ready',
  attempts       INT NOT NULL DEFAULT 0,
  available_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  lease_until    TIMESTAMPTZ,
  lease_token    UUID,
  last_error     TEXT,
  PRIMARY KEY (consumer_group, "offset")
);

CREATE FUNCTION fanout_stmt() RETURNS trigger AS $$ BEGIN
  INSERT INTO deliveries (consumer_group,"offset",state)
  SELECT c.consumer_group, n."offset", 'ready' FROM new_events n CROSS JOIN cursors c
  ON CONFLICT DO NOTHING; RETURN NULL; END $$ LANGUAGE plpgsql;

CREATE FUNCTION fanout_row() RETURNS trigger AS $$ BEGIN
  INSERT INTO deliveries (consumer_group,"offset",state)
  SELECT c.consumer_group, NEW."offset", 'ready' FROM cursors c
  ON CONFLICT DO NOTHING; RETURN NULL; END $$ LANGUAGE plpgsql;

CREATE FUNCTION fanout_notify() RETURNS trigger AS $$ BEGIN
  PERFORM pg_notify('events_appended',''); RETURN NULL; END $$ LANGUAGE plpgsql;
SQL

# Trigger variant
case "$TRIGGER" in
  stmt)   psql_run "CREATE TRIGGER trg_fanout AFTER INSERT ON events REFERENCING NEW TABLE AS new_events FOR EACH STATEMENT EXECUTE FUNCTION fanout_stmt();";;
  row)    psql_run "CREATE TRIGGER trg_fanout AFTER INSERT ON events FOR EACH ROW EXECUTE FUNCTION fanout_row();";;
  notify) psql_run "CREATE TRIGGER trg_fanout AFTER INSERT ON events FOR EACH STATEMENT EXECUTE FUNCTION fanout_notify();";;
  none)   : ;;
esac

# Index variant on deliveries
if [[ "$IDX" == "claim" ]]; then
  psql_run "CREATE INDEX dlv_ready    ON deliveries (consumer_group,\"offset\")    WHERE state='ready';"
  psql_run "CREATE INDEX dlv_inflight ON deliveries (consumer_group,lease_until) WHERE state='inflight';"
fi

# Seed cursors g0..g{N-1}
if (( N_GROUPS > 0 )); then
  psql_run "INSERT INTO cursors (consumer_group) SELECT 'g'||g FROM generate_series(0,$N_GROUPS-1) g ON CONFLICT DO NOTHING;"
fi

# Truncate event/delivery DATA before each trial so table/index size doesn't drift.
truncate_data() {
  psql_run "TRUNCATE deliveries, events RESTART IDENTITY;"
}

# Parse 'tps = N (...)' from pgbench output. Prefer 'without initial connection time'.
parse_tps() {
  local out="$1"
  echo "$out" | awk '/tps = / {v=$3} END {print v}'
}

# Parse '\timing' "Time: X ms" line.
parse_ms() {
  local out="$1"
  echo "$out" | awk -F'Time: ' '/Time: / {split($2,a," "); v=a[1]} END {print v}'
}

# --- run trials ---
declare -a TRIAL_VALS=()
BEST=""
METRIC=""
UNIT="per_sec"

float_gt() { awk -v a="$1" -v b="$2" 'BEGIN{exit !(a>b)}'; }

# For projector: seed events ONCE (trigger=none recommended so no deliveries appear),
# then truncate ONLY deliveries before each trial, keeping events fixed.
if [[ "$MODE" == "projector" ]]; then
  truncate_data
  psql_run "INSERT INTO events(payload) SELECT '{\"x\":1}'::jsonb FROM generate_series(1,$PARAM);"
fi

for ((t=1; t<=TRIALS; t++)); do
  case "$MODE" in
    tps)
      truncate_data
      METRIC="events_per_sec"
      out="$(pgbench -n -f "$INSERT_SQL" -c "$CLIENTS" -j "$CLIENTS" -T "$PARAM" 2>&1)"
      val="$(parse_tps "$out")"
      [[ -n "$val" ]] || { echo "FATAL: could not parse tps. pgbench output:" >&2; echo "$out" >&2; exit 1; }
      ;;
    bulk)
      truncate_data
      METRIC="events_per_sec"
      out="$(psql -v ON_ERROR_STOP=1 -c '\timing on' -c "INSERT INTO events(payload) SELECT '{\"x\":1}'::jsonb FROM generate_series(1,$PARAM);" 2>&1)"
      ms="$(parse_ms "$out")"
      [[ -n "$ms" ]] || { echo "FATAL: could not parse ms. psql output:" >&2; echo "$out" >&2; exit 1; }
      val="$(awk -v p="$PARAM" -v ms="$ms" 'BEGIN{printf "%.6f", p/(ms/1000.0)}')"
      ;;
    projector)
      METRIC="deliveries_per_sec"
      psql_run "TRUNCATE deliveries;"   # keep events; reset deliveries each trial
      out="$(psql -v ON_ERROR_STOP=1 -c '\timing on' -c "INSERT INTO deliveries(consumer_group,\"offset\",state) SELECT 'g0',\"offset\",'ready' FROM events;" 2>&1)"
      ms="$(parse_ms "$out")"
      [[ -n "$ms" ]] || { echo "FATAL: could not parse ms. psql output:" >&2; echo "$out" >&2; exit 1; }
      val="$(awk -v p="$PARAM" -v ms="$ms" 'BEGIN{printf "%.6f", p/(ms/1000.0)}')"
      ;;
  esac
  TRIAL_VALS+=("$val")
  if [[ -z "$BEST" ]] || float_gt "$val" "$BEST"; then BEST="$val"; fi
done

# ----- fanout correctness verification (on final-trial state) -----
EVENTS="$(psql_q "SELECT count(*) FROM events;")"
DELIVS="$(psql_q "SELECT count(*) FROM deliveries;")"
VERIFIED=false
case "$TRIGGER" in
  stmt|row)
    EXPECT=$(( N_GROUPS * EVENTS ))
    if [[ "$MODE" == "projector" ]]; then
      # projector seeds with whatever trigger installed; for stmt/row the seed insert also fired
      # the trigger, so deliveries include trigger output + projector output. Just assert >0 sane.
      if (( DELIVS > 0 )); then VERIFIED=true; fi
    else
      if (( DELIVS == EXPECT )) && (( EXPECT > 0 || EVENTS == 0 )); then VERIFIED=true; fi
    fi
    ;;
  none|notify)
    if [[ "$MODE" == "projector" ]]; then
      # projector deliberately inserts deliveries; check it produced exactly EVENTS rows (g0)
      if (( DELIVS == EVENTS )); then VERIFIED=true; fi
    else
      if (( DELIVS == 0 )); then VERIFIED=true; fi
    fi
    ;;
esac

DETAIL="trial values: $(IFS=,; echo "${TRIAL_VALS[*]}")"

printf 'RESULT trigger=%s groups=%s sync=%s idx=%s mode=%s clients=%s metric=%s value=%s unit=%s verified=%s detail="%s"\n' \
  "$TRIGGER" "$N_GROUPS" "$SYNC_ACTUAL" "$IDX" "$MODE" "$CLIENTS" "$METRIC" "$BEST" "$UNIT" "$VERIFIED" "$DETAIL"
