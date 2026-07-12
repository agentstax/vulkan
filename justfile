set dotenv-required := true

### DATABASE ###

database-up:
  docker-compose -f ./scripts/database/docker-compose.yaml up

database-down:
  docker-compose -f ./scripts/database/docker-compose.yaml down

database-delete:
  docker-compose -f ./scripts/database/docker-compose.yaml down -v

migrate-up:
  migrate -source file:./migrations -database postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@localhost:${POSTGRES_PORT}/${POSTGRES_DB}?sslmode=disable up

migrate-down:
  migrate -source file:./migrations -database postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@localhost:${POSTGRES_PORT}/${POSTGRES_DB}?sslmode=disable down

### TESTING ###

# EX: just consume
consume group="learning.v1" processorsleep="0.1" shutdownsleep="1.0" failrate="0.0" crashafter="-1":
  go run examples/phase_1/consumer/main.go -group={{ group }} -processor-sleep={{ processorsleep }} -shutdown-sleep={{ shutdownsleep }} -fail-rate={{ failrate }} -crash-after={{ crashafter }}

# EX: just produce 3
produce count="1":
  go run examples/phase_1/producer/main.go -count={{ count }}

# Phase 6.5b lab: crash mid-range, recover. Deterministic, self-verifying,
# self-seeding -- registers its own topic and publishes its own backlog.
reclaim-lab:
  go run examples/phase_1/reclaimlab/main.go

# Phase 6.5c lab: waterline pins on a failing message, jumps past it once resolved.
# Deterministic, self-verifying, self-seeding -- registers its own topic and
# publishes its own backlog.
exception-lab:
  go run examples/phase_1/exceptionlab/main.go

# routing lab: bindings gate what a group receives, not what gets claimed.
# Deterministic, self-verifying, self-seeding -- registers its own topic and
# publishes its own messages.
routing-lab:
  go run examples/phase_1/routinglab/main.go

# Phase 8a lab (a): id-range partitioning prunes claim reads to 1-2 partitions.
# Self-contained -- registers its own topic at a lab-scale partition width
# (Phase 8b made partition width a per-topic Register() param, so no more
# schema-swap/data-wipe of a shared message_log); the topic is destroyed on exit.
partition-lab:
  go run examples/phase_1/partitionlab/main.go

# Phase 8a lab (b): a dropped partition is a hole a lagging cursor walks over
# empty, not a stall; the drop floor refuses the drop until committed past it
# or waived. Same per-topic isolation as partition-lab, no shared-table caveat.
drop-floor-lab:
  go run examples/phase_1/dropfloorlab/main.go

# Phase 8a lab (c): the low-volume tail -- a partition too small to ever earn
# a whole-partition drop still sheds its expired prefix via the sweep.
# Registers its own topic at the real migration-shipped partition width, so
# it never rolls to a second partition -- exactly the condition the sweep covers.
sweep-lab:
  go run examples/phase_1/sweeplab/main.go

# Phase 8b's own lab: proves per-topic tables/sequences are independent, a
# lagging group's floor stays inside its own topic, routing still works
# scoped to one topic, two slices sharing one topic still share its floor
# (deliberately not fixed), and an unregistered topic id fails clearly.
topic-lab:
  go run examples/phase_1/topiclab/main.go

# log compaction lab: latest-per-key survives a claim, older rows stay
# physically present, a delivered version isn't retroactively unsent, the
# crash/reclaim race gives a superseded row zero delivery while its successor
# still gets its own, tombstones are a pure app convention on both paths, and
# unkeyed reads never pay the compaction subplan's cost.
compaction-lab:
  go run examples/phase_1/compactionlab/main.go

# log compaction width/planner lab: measures whether proving a row IS the
# latest for its key (no early termination) actually costs more partition
# scans than proving it ISN'T (can stop at the first match) -- and whether a
# coarser PartitionSize collapses that cost. Registers two identically-seeded
# topics differing only in PartitionSize, EXPLAIN ANALYZEs both cases on each.
compaction-width-lab:
  go run examples/phase_1/compactionwidthlab/main.go

# log compaction SCALE lab: how bad "prove a negative" gets as a topic's
# history grows -- the backlog-replay worst case, not the small A/B width
# comparison compaction-width-lab runs. One never-superseded row is
# re-measured fresh at each checkpoint as more partitions/rows pile up
# behind it, tracking a genuine growth curve (partitions touched + wall
# clock) instead of one snapshot.
compaction-scale-lab:
  go run examples/phase_1/compactionscalelab/main.go

# latest_keys correctness lab: N goroutines publish to the SAME key at once,
# proving the write path's id-guard converges to the true max regardless of
# commit order -- plus the O(1) counterpart to compaction-scale-lab's linear
# curve, same checkpoints, EXPLAIN ANALYZEing the NEW latest_keys lookup
# instead of the old scan. Touched partitions must stay flat at every size.
latest-keys-race-lab:
  go run examples/phase_1/latestkeysracelab/main.go

# latest_keys + retention lab: does 8a's retention correctly garbage collect
# latest_keys when it reaps a compacted key's last surviving row? Covers both
# janitor paths (dropPartition's whole-partition removal, sweepBatch's
# individually-expired-row reap) and confirms a key touched inside the ttl
# window survives every pass untouched, either path.
latest-keys-retention-lab:
  go run examples/phase_1/latestkeysretentionlab/main.go

# latest_keys write-cost lab: quantifies the tradeoff -- an O(1) read path
# cost a second write on every keyed publish. Sequential/uncontended cost vs.
# an unkeyed baseline, hot-key lock contention under concurrency (many
# distinct keys vs. all publishers hammering ONE key), and the dead-tuple
# growth that contention leaves behind for autovacuum.
latest-keys-write-lab:
  go run examples/phase_1/latestkeyswritelab/main.go

# idempotency_keys lab: does AppendMessage's retry-safety claim gate actually
# prevent a double-publish, and does its cleanup actually drain it? Covers a
# retried AppendMessage under the same key (must land exactly once), distinct
# keys (must never collide), an unset key (must protect only within one
# call, not dedupe separate publishes), the sweep (expired claims drained in
# bounded batches, live ones survive), and IdempotencyKeyTTL surviving a
# topic re-registration unchanged.
idempotency-keys-lab:
  go run examples/phase_1/idempotencykeyslab/main.go

# idempotency_keys growth lab: the sustained-throughput/storage axis of the
# claim-gate tradeoff. Measures relative storage overhead vs. message_log
# with no sweep running, then proves the janitor's real sweep cadence keeps
# the table's steady-state size bounded near Little's Law's rate*ttl instead
# of growing toward the full published count, and drains to zero afterward.
idempotency-keys-growth-lab:
  go run examples/phase_1/idempotencykeysgrowthlab/main.go

# DeleteTopic cascade lab: seeds a row in every topic-scoped table
# (cursors, leases, deliveries, bindings, latest_keys, idempotency_keys) --
# including a still-open lease and an unclaimed deliveries row, not just the
# already-resolved case -- then confirms Destroy cleans up all six, not just
# message_log and the topics row itself.
delete-topic-lab:
  go run examples/phase_1/deletetopiclab/main.go

# EX: just peek 1
peek topic_id:
  psql "postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@localhost:${POSTGRES_PORT}/${POSTGRES_DB}?sslmode=disable" \
    -c "SELECT * FROM message_log_{{ topic_id }} ORDER BY id;"

peek-users:
  psql "postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@localhost:${POSTGRES_PORT}/${POSTGRES_DB}?sslmode=disable" \
    -c "SELECT * FROM users ORDER BY id;"

# Phase 5 health metric: per-group lag = log head − cursor position, scoped to one topic.
# Run two groups, slow one with -processorsleep, watch their lags diverge.
# EX: just lag 1
lag topic_id:
  psql "postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@localhost:${POSTGRES_PORT}/${POSTGRES_DB}?sslmode=disable" \
    -c "SELECT c.consumer_group, c.claimed, COALESCE((SELECT max(id) FROM message_log_{{ topic_id }}), 0) AS head, COALESCE((SELECT max(id) FROM message_log_{{ topic_id }}), 0) - c.claimed AS lag FROM cursors c WHERE c.topic_id = {{ topic_id }} ORDER BY lag DESC;"

### DOC SITE (https://vulkan-5ss.pages.dev) ###

site-dev:
  cd website && npm run dev

site-deploy:
  cd website && npm run build && ./node_modules/.bin/wrangler pages deploy dist --project-name vulkan --branch main