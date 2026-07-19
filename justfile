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

# idempotency_keys race lab: N goroutines sharing one idempotency key must
# land exactly once under true concurrency (not just sequential retries),
# and N goroutines each with their own distinct key must all land -- mirrors
# latestkeysracelab's concurrent-race precedent.
idempotency-keys-race-lab:
  go run examples/phase_1/idempotencykeysracelab/main.go

# DeleteTopic cascade lab: seeds a row in every topic_id-scoped table
# (cursors, leases, bindings, latest_keys) plus the per-topic deliveries and
# idempotency_keys tables -- including a still-open lease and an unclaimed
# deliveries row, not just the already-resolved case -- then confirms
# Destroy cleans up all of them, not just message_log and the topics row
# itself.
delete-topic-lab:
  go run examples/phase_1/deletetopiclab/main.go

# delivery_log lab: a fresh failure logs exactly one row (right attempt
# number + error), a success logs none, and two retries of the same message
# append two MORE distinct rows (attempt=1, attempt=2) rather than
# overwriting -- the (consumer_group, message_id, attempt) PK makes that
# structural, not incidental. Also covers the opt-out (DisableDeliveryLog
# skips table creation and every write) and retention (dropPartition/
# sweepBatch drain delivery_log the same as they already drain delivery_<id>).
delivery-log-lab:
  go run examples/phase_1/deliveryloglab/main.go

# Phase 9 lab: graceful-shutdown lease truncation. A shutdown signal mid-range
# stops CursorClaim from taking on new messages, but everything already
# resolved (successes + a parked exception) survives and the lease narrows to
# just the untouched suffix -- confirms the resolved prefix is never
# redelivered, the waterline's exception-blocker and lease-narrowing terms
# combine correctly via LEAST, and the untouched suffix reclaims on its own.
shutdown-truncation-lab:
  go run examples/phase_1/shutdowntruncationlab/main.go

# Phase 9 lab: induces all three failure modes through the real CursorClaim
# path -- a panicking consumerFunc, one that hangs past WorkTimeout, and a
# forced pkg/retry transient blip. Confirms each is isolated to the one
# message/call it happened on: the panic and the hang each dead-letter/retry
# just that message without blocking the other two in the same batch, the
# abandoned-goroutine gauge tracks the hang while it's still running in the
# background and self-clears once it finishes, and a DB blip that clears
# within MaxRetries is fully invisible to the caller (building this lab
# surfaced and fixed a real bug in pkg/retry.Wrap swallowing a successful
# retry behind its own prior failures).
fault-isolation-lab:
  go run examples/phase_1/faultisolationlab/main.go

# Phase 10 lab: measures the lazy-vs-synchronous AdvanceWaterline tradeoff.
# Staleness (time from Commit to `committed` reflecting it: periodic roller
# tick vs. calling AdvanceWaterline synchronously right after Commit), fixed
# per-op cost of the extra round trip uncontended, and the contention cost of
# a synchronous call hammering the same (group, topic) cursors row Commit
# itself never touches today.
rollup-lab:
  go run examples/phase_1/rolluplab/main.go

# Phase 10 lab: drives each failure mode the harness flags (--fail-rate,
# --sleep, --crash-after) represent through the real WorkConsumer/Datastore
# paths and diffs the metrics snapshot before/after -- asserts each induced
# failure moves EXACTLY the number(s) it should and nothing else, the
# executable form of LEARNING_PLAN.md's "if a failure doesn't move a number,
# you have a blind spot" check.
metrics-reaction-lab:
  go run examples/phase_1/metricsreactionlab/main.go

# Phase 10 lab: runs the real Consume loop under load with the debug readout
# on. Measures committed-catch-up wall-clock time at a slow vs. fast
# WaterlinePollRate (the live, end-to-end counterpart to rollup-lab's direct-
# datastore-call staleness measurement), then a second run with injected
# retryable failures -- watches exception counts rise and fully drain back
# to zero live, with no manual intervention.
metrics-load-lab:
  go run examples/phase_1/metricsloadlab/main.go

# Phase 10 lab: the only place in the repo that imports otel/sdk or a
# specific exporter. Wires a real sdkmetric.MeterProvider backed by
# otel/exporters/prometheus's Reader, drives real consumer activity
# (including a hard-timeout abandon+self-clear so the synchronous
# AbandonedRoutines instruments have a data point too, not just the
# always-live QueueState gauges), then scrapes over a real HTTP server via
# promhttp.HandlerFor -- confirms every instrument pkg/consumer/metrics
# registers actually shows up on the wire, not just that it compiles against
# the otel/metric API.
otel-export-lab:
  go run examples/phase_1/otelexportlab/main.go

# multi-target transactional enqueue lab: two targets published inside one
# producer.InTransaction closure commit together, a failure on either rolls
# back both (not just the failing one), a missing-partition self-heal on one
# target never touches the other's already-made insert or reruns a caller
# side effect between the two calls, and a Commit-time failure surfaces
# completely unclassified -- no retry.PermanentError wrapping -- regardless
# of SkipIdempotency mix across targets.
multi-target-lab:
  go run examples/phase_1/multitargetlab/main.go

# producer batch lab: the batched payload-only Produce path. Concurrent
# callers share transactions (xmin-proven) and land exactly once, a
# caller-keyed call routes per-call and dedups, a poisoned/unencodable
# payload fails only its own caller, hot compaction keys never deadlock
# across concurrent batches, bursts self-heal missing partitions, and a
# timing pass (batched vs per-call at equal concurrency, plus a saturated
# batched arm) reports what the fsync amortization actually buys in-library.
producer-batch-lab:
  go run examples/phase_1/producerbatchlab/main.go

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