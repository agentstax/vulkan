#!/usr/bin/env bash
# Full sweep for the compaction hot-key serialization benchmark.
# Assumes vulkan-bench-pg is up (container.sh start). Appends one JSON line
# per cell to results/cells.jsonl.
#
# Matrix:
#   primary   sync=on  producers=3 goroutines=128: cardinality 0(unkeyed)/1024/64/8/2/1
#   fsync     sync=off producers=3 goroutines=128: same cardinalities
#   producers sync=on  cardinality 1 and 0: producers 1 and 6 (goroutines 128)
set -euo pipefail
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"; source "$DIR/env.sh"

OUT="$DIR/results/cells.jsonl"
mkdir -p "$DIR/results"

cell() {
  local sync="$1" cardinality="$2" producers="$3" label="$4"
  psql_run "ALTER DATABASE \"$PGDATABASE\" SET synchronous_commit = $sync;"
  echo ">>> $label (sync=$sync cardinality=$cardinality producers=$producers)" >&2
  (cd "$DIR" && go run ./driver \
    -cardinality "$cardinality" -producers "$producers" -goroutines 128 \
    -warmup 5 -window 15 -label "$label") | tee -a "$OUT"
}

for cardinality in 0 1024 64 8 2 1; do
  cell on "$cardinality" 3 "primary"
done

for cardinality in 0 1024 64 8 2 1; do
  cell off "$cardinality" 3 "fsync-isolation"
done

for producers in 1 6; do
  cell on 1 "$producers" "producer-count"
  cell on 0 "$producers" "producer-count"
done

psql_run "ALTER DATABASE \"$PGDATABASE\" SET synchronous_commit = on;"
echo "sweep complete -> $OUT" >&2
