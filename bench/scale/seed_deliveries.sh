#!/usr/bin/env bash
# Pre-grow the deliveries table: materialize one 'ready' row per (cursor, event)
# for ALL current events, in CHUNK-by-offset batches. Used to set up a fixed
# backlog for consumer-drain tests on materialize designs (d2/d3) and to reach a
# target deliveries-table size.
#   Usage: seed_deliveries.sh [CHUNK_OFFSETS]
set -euo pipefail
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"; source "$DIR/env.sh"
CHUNK="${1:-1000000}"

HEAD="$(psql_q "SELECT COALESCE(max(\"offset\"),0) FROM events;")"
lo=0
while (( lo < HEAD )); do
  hi=$(( lo + CHUNK )); (( hi > HEAD )) && hi="$HEAD"
  psql_run "INSERT INTO deliveries(consumer_group,\"offset\",state)
            SELECT c.consumer_group, e.\"offset\", 'ready'
            FROM cursors c CROSS JOIN events e
            WHERE e.\"offset\" > $lo AND e.\"offset\" <= $hi
            ON CONFLICT DO NOTHING;"
  lo="$hi"
done
# settle WAL + refresh planner stats so back-to-back re-arms don't pile up a
# checkpoint storm (which stalls the next drain run).
psql_run "CHECKPOINT;"
psql_run "ANALYZE deliveries;"
echo "seeded deliveries: $(psql_q "SELECT count(*) FROM deliveries;") (events=$HEAD x groups)"
