#!/usr/bin/env bash
# Build a clean schema for a given design.
#   Usage: reset.sh DESIGN N_GROUPS [IDX]
#     DESIGN   d1|d2|d3|d4|d5
#     N_GROUPS number of consumer groups (cursors g0..g{N-1})
#     IDX      claim|none  (deliveries claim indexes; default claim)
#
# Tables are uniform across designs (harmless if unused) so measure.sh can rely
# on them. Design-specific bits: d1 installs the statement-level fanout trigger;
# d1/d2/d3 get deliveries claim indexes; d5 uses the leases table.
#
# NOTE: never name a shell var literally GROUPS — some harness shells pre-set it
# readonly (=20), silently shadowing the CLI value. We use N_GROUPS.
set -euo pipefail
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"; source "$DIR/env.sh"

DESIGN="${1:?DESIGN d1|d2|d3|d4|d5}"
N_GROUPS="${2:?N_GROUPS int}"
IDX="${3:-claim}"

case "$DESIGN" in d1|d2|d3|d4|d5) ;; *) echo "bad DESIGN: $DESIGN" >&2; exit 2;; esac
[[ "$N_GROUPS" =~ ^[0-9]+$ ]] || { echo "N_GROUPS must be int" >&2; exit 2; }
case "$IDX" in claim|none) ;; *) echo "bad IDX: $IDX" >&2; exit 2;; esac

psql -v ON_ERROR_STOP=1 -q <<SQL
DROP TRIGGER IF EXISTS trg_fanout ON events;
DROP TABLE IF EXISTS deliveries CASCADE;
DROP TABLE IF EXISTS leases CASCADE;
DROP TABLE IF EXISTS events CASCADE;
DROP TABLE IF EXISTS cursors CASCADE;
DROP TABLE IF EXISTS progress CASCADE;
DROP FUNCTION IF EXISTS fanout_stmt() CASCADE;
DROP FUNCTION IF EXISTS notify_stmt() CASCADE;

CREATE TABLE events (
  "offset"   BIGSERIAL PRIMARY KEY,
  payload    JSONB NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE cursors (
  consumer_group TEXT PRIMARY KEY,
  committed BIGINT NOT NULL DEFAULT 0,   -- waterline: all <= committed are done
  projected BIGINT NOT NULL DEFAULT 0,   -- fanout frontier (materialize designs)
  claimed   BIGINT NOT NULL DEFAULT 0    -- claim frontier (claim-from-log designs)
);

CREATE TABLE deliveries (
  consumer_group TEXT NOT NULL,
  "offset"       BIGINT NOT NULL,
  state          TEXT NOT NULL DEFAULT 'ready',  -- ready|inflight|acked|dead
  attempts       INT  NOT NULL DEFAULT 0,
  lease_until    TIMESTAMPTZ,
  lease_token    BIGINT,
  last_error     TEXT,
  PRIMARY KEY (consumer_group, "offset")
);

CREATE TABLE leases (                            -- design 5 (range-lease)
  consumer_group TEXT NOT NULL,
  lo             BIGINT NOT NULL,
  hi             BIGINT NOT NULL,
  lease_until    TIMESTAMPTZ,
  lease_token    BIGINT,
  created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (consumer_group, lo)
);

-- universal completion counter (sharded to avoid hot-row contention)
CREATE TABLE progress (shard INT PRIMARY KEY, n BIGINT NOT NULL DEFAULT 0);
INSERT INTO progress(shard) SELECT generate_series(0, ${NSHARDS} - 1);

-- statement-level set-based fanout: one delivery row per (cursor, new event)
CREATE FUNCTION fanout_stmt() RETURNS trigger AS \$\$ BEGIN
  INSERT INTO deliveries (consumer_group, "offset", state)
  SELECT c.consumer_group, n."offset", 'ready'
  FROM new_events n CROSS JOIN cursors c
  ON CONFLICT DO NOTHING;
  RETURN NULL;
END \$\$ LANGUAGE plpgsql;

-- coalesced notify: one per INSERT statement (never per row) -> design 2
CREATE FUNCTION notify_stmt() RETURNS trigger AS \$\$ BEGIN
  PERFORM pg_notify('events_appended', '');
  RETURN NULL;
END \$\$ LANGUAGE plpgsql;
SQL

# d1: install the synchronous statement-level fanout trigger
if [[ "$DESIGN" == "d1" ]]; then
  psql_run "CREATE TRIGGER trg_fanout AFTER INSERT ON events REFERENCING NEW TABLE AS new_events FOR EACH STATEMENT EXECUTE FUNCTION fanout_stmt();"
fi

# d2: install the coalesced NOTIFY trigger (projector materializes asynchronously)
if [[ "$DESIGN" == "d2" ]]; then
  psql_run "CREATE TRIGGER trg_notify AFTER INSERT ON events FOR EACH STATEMENT EXECUTE FUNCTION notify_stmt();"
fi

# claim indexes for materialize designs
if [[ "$IDX" == "claim" && "$DESIGN" =~ ^d[123]$ ]]; then
  psql_run "CREATE INDEX dlv_ready ON deliveries (\"offset\") WHERE state='ready';"
  psql_run "CREATE INDEX dlv_tok   ON deliveries (lease_token) WHERE state='inflight';"
fi

# seed cursors g0..g{N-1}
if (( N_GROUPS > 0 )); then
  psql_run "INSERT INTO cursors (consumer_group) SELECT 'g'||g FROM generate_series(0, $N_GROUPS - 1) g ON CONFLICT DO NOTHING;"
fi

echo "reset ok: design=$DESIGN groups=$N_GROUPS idx=$IDX nshards=$NSHARDS"
