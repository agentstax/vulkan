#!/usr/bin/env bash
# Append N_EVENTS to the events log in CHUNK-sized statements.
# Fires the d1 fanout trigger if installed (so this also pre-grows deliveries for d1).
#   Usage: seed_events.sh N_EVENTS [CHUNK]
set -euo pipefail
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"; source "$DIR/env.sh"
N="${1:?N_EVENTS}"; CHUNK="${2:-1000000}"
[[ "$N" =~ ^[0-9]+$ ]] || { echo "N_EVENTS must be int" >&2; exit 2; }

remaining="$N"
while (( remaining > 0 )); do
  c="$CHUNK"; (( c > remaining )) && c="$remaining"
  psql_run "INSERT INTO events(payload) SELECT '{\"x\":1}'::jsonb FROM generate_series(1, $c);"
  remaining=$(( remaining - c ))
done
echo "seeded events: $(psql_q "SELECT count(*) FROM events;")"
