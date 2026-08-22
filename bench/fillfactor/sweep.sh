#!/usr/bin/env bash
# Full sweep for the consume-side fillfactor benchmark.
# Assumes vulkan-bench-pg is up (container.sh start). Appends one JSON line
# per cell to results/cells.jsonl.
#
# Matrix (sync=on throughout; fillfactor 0 = table default 100, the baseline):
#   cursor-batch1   failure-rate 0    batch-limit 1:   cursor fillfactor 0/50/20
#   cursor-batch100 failure-rate 0    batch-limit 100: cursor fillfactor 0/20
#   delivery        failure-rate 0.5  batch-limit 100: delivery fillfactor 0/90/70
set -euo pipefail
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"; source "$DIR/env.sh"

OUT="$DIR/results/cells.jsonl"
mkdir -p "$DIR/results"

cell() {
  local label="$1"; shift
  echo ">>> $label $*" >&2
  (cd "$DIR" && go run ./driver -warmup 10 -window 15 -label "$label" "$@") | tee -a "$OUT"
}

psql_run "ALTER DATABASE \"$PGDATABASE\" SET synchronous_commit = on;"

# one cursor update per message claimed -- maximal cursor churn
for fillfactor in 0 50 20; do
  cell cursor-batch1 -batch-limit 1 -cursor-fillfactor "$fillfactor"
done

# batched claims -- the realistic claim shape
for fillfactor in 0 20; do
  cell cursor-batch100 -batch-limit 100 -cursor-fillfactor "$fillfactor"
done

# half the messages fail every attempt -- exception-window churn on delivery
for fillfactor in 0 90 70; do
  cell delivery -batch-limit 100 -failure-rate 0.5 -delivery-fillfactor "$fillfactor"
done

echo "sweep complete -> $OUT" >&2
