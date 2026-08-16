set dotenv-required := true

### DATABASE ###

database-up:
  docker-compose -f ./scripts/database/docker-compose.yaml up

database-down:
  docker-compose -f ./scripts/database/docker-compose.yaml down

database-delete:
  docker-compose -f ./scripts/database/docker-compose.yaml down -v

# dev bootstrap: stands up the control-plane schema in Go (RegisterSystem).
# Idempotent -- safe to re-run.
system-register:
  go run examples/phase_1/systemregister/main.go

### TESTING ###

# EX: just consume
consume group="learning.v1" processorsleep="0.1" shutdownsleep="1.0" failrate="0.0" crashafter="-1":
  go run examples/phase_1/consumer/main.go -group={{ group }} -processor-sleep={{ processorsleep }} -shutdown-sleep={{ shutdownsleep }} -fail-rate={{ failrate }} -crash-after={{ crashafter }}

# EX: just produce 3
produce count="1":
  go run examples/phase_1/producer/main.go -count={{ count }}

# build a lab binary into bin/ (EX: just build-lab reclaimlab)
build-lab lab:
  go build -o bin/{{ lab }} examples/phase_1/{{ lab }}/main.go

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

# binding lifecycle lab: sets declared at consumer Register -- same-set join,
# a divergent set waiting on a live incumbent, and the dead-fleet swap that
# ends the wait, consuming under the new set.
binding-lab:
  go run examples/phase_1/bindinglab/main.go

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

reserved-topic-lab:
  go run examples/phase_1/reservedtopiclab/main.go

abandoned-routine-snapshot-lab:
  go run examples/phase_1/abandonedroutinesnapshotlab/main.go

metrics-lab:
  go run examples/phase_1/metricslab/main.go

# metrics collector lab: a full-size collection pass under -race (topic
# fan-out under TopicConcurrency, each group's measurements produced
# concurrently on one ProducerInstance), heads/history read through the
# admin verbs the CLI renders, and a real `vulkan manager run
# --metrics-address` process scraped over HTTP.
metrics-collector-lab:
  go build -o bin/vulkan ./cmd/vulkan
  go run -race examples/phase_1/metricscollectorlab/main.go

abandoned-events-lab:
  go run examples/phase_1/abandonedeventslab/main.go

duty-backoff-lab:
  go run examples/phase_1/dutybackofflab/main.go

# default-alert lab: RegisterSystem seeding + declared thresholds applying,
# every classify arm (edge WARN, quiet hold, repeat republish refreshing the
# head, silent severity change, resolve INFO), the live partition_count
# executor end to end, and per-topic isolation around a corrupted head.
alert-lab:
  go run examples/phase_1/alertlab/main.go

# cron lab: registration validation (charset, Feb-29, timeout-vs-rate),
# owner cascade vs standalone survival, produce-once newest-due walk, v7
# dedupe on a re-backdated scheduled time, suspend/unsuspend, a poisoned row
# skipped while siblings produce, defer behind a running request, run-now
# default-allow vs cfg-defer, run-now superseding a pending unclaimed
# request, and consumer end-to-end with per-group status + request listing.
cron-lab:
  go run examples/phase_1/cronlab/main.go

key-lease-lab:
  go run examples/phase_1/keyleaselab/main.go

defer-lab:
  go run examples/phase_1/deferlab/main.go

# log compaction lab: latest-per-key survives a claim, older rows stay
# physically present, a delivered version isn't retroactively unsent, the
# crash/reclaim race gives a superseded row zero delivery while its successor
# still gets its own, tombstones are a pure app convention on both paths, and
# unkeyed reads never pay the compaction subplan's cost.
compaction-lab:
  go run examples/phase_1/compactionlab/main.go

# CompactionRank lab: a rank-100 pin ignores every normal-rank update after
# it even at a higher id, a -1 backfill write never beats a live rank-0
# write regardless of arrival order (the bridge's exact interleaving, both
# orderings), and every losing row stays physically present but never claimed.
compaction-rank-lab:
  go run examples/phase_1/compactionranklab/main.go

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

# compaction_head correctness lab: N goroutines publish to the SAME key at once,
# proving the write path's id-guard converges to the true max regardless of
# commit order -- plus the O(1) counterpart to compaction-scale-lab's linear
# curve, same checkpoints, EXPLAIN ANALYZEing the NEW compaction_head lookup
# instead of the old scan. Touched partitions must stay flat at every size.
compaction-head-race-lab:
  go run examples/phase_1/compactionheadracelab/main.go

# compaction_head + retention lab: does 8a's retention correctly garbage collect
# compaction_head when it reaps a compacted key's last surviving row? Covers both
# janitor paths (dropPartition's whole-partition removal, sweepBatch's
# individually-expired-row reap) and confirms a key touched inside the ttl
# window survives every pass untouched, either path.
compaction-head-retention-lab:
  go run examples/phase_1/compactionheadretentionlab/main.go

# compaction_head write-cost lab: quantifies the tradeoff -- an O(1) read path
# cost a second write on every keyed publish. Sequential/uncontended cost vs.
# an unkeyed baseline, hot-key lock contention under concurrency (many
# distinct keys vs. all publishers hammering ONE key), and the dead-tuple
# growth that contention leaves behind for autovacuum.
compaction-head-write-lab:
  go run examples/phase_1/compactionheadwritelab/main.go

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
# compactionheadracelab's concurrent-race precedent.
idempotency-keys-race-lab:
  go run examples/phase_1/idempotencykeysracelab/main.go

# DeleteTopic cascade lab: seeds a row in every topic_id-scoped table
# (cursors, leases, bindings, compaction_head) plus the per-topic deliveries and
# idempotency_keys tables -- including a still-open lease and an unclaimed
# deliveries row, not just the already-resolved case -- then confirms
# Destroy cleans up all of them, not just message_log and the topics row
# itself.
delete-topic-lab:
  go run examples/phase_1/deletetopiclab/main.go

# destroy-system lab: DestroySystem is RegisterSystem's inverse [0514] --
# a registered user topic and a running consumer each refuse the unforced
# destroy (worker guard outranks the topic guard), the clean destroy drops
# every control-plane table, and RegisterSystem stands the schema back up.
destroy-system-lab:
  go run examples/phase_1/destroysystemlab/main.go

# register idempotency lab: re-registering a topic is idempotent (same config
# resolves to the same topic, not an error) and a conflicting config is
# rejected with ErrTopicConfigMismatch -- guards the created_at/updated_at
# struct-equality edge in topic.upsertTopic. Self-seeding, destroyed on exit.
register-idempotency-lab:
  go run examples/phase_1/registeridempotencylab/main.go

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

# Phase 10 lab: measures the lazy-vs-synchronous AdvanceWaterline tradeoff.
# Staleness (time from Commit to `committed` reflecting it: periodic roller
# tick vs. calling AdvanceWaterline synchronously right after Commit), fixed
# per-op cost of the extra round trip uncontended, and the contention cost of
# a synchronous call hammering the same (group, topic) cursors row Commit
# itself never touches today.
rollup-lab:
  go run examples/phase_1/rolluplab/main.go

# multi-target transactional enqueue lab: two targets published inside one
# producer.InTransaction closure commit together, a failure on either rolls
# back both (not just the failing one), a missing-partition self-heal on one
# target never touches the other's already-made insert or reruns a caller
# side effect between the two calls, a Commit-time failure surfaces
# completely unclassified (no retry.PermanentError wrapping -- retrying is
# the caller's decision), and rerunning the closure under caller-supplied
# IdempotencyKeys dedups every target instead of double-publishing.
multi-target-lab:
  go run examples/phase_1/multitargetlab/main.go

# invariant lab: the migrate engine's guarantees under a fixture registry (the
# real registries are empty) -- migrate-to-N == fresh-create-at-N via an
# information_schema diff, up->down->up reversibility, and Up/Down idempotency
# under an ambiguous-commit re-run. The linear-history teeth golang-migrate's
# file layout used to give for free. Borrows the system entity against scratch
# tables, resets to baseline on exit.
invariant-lab:
  go run examples/phase_1/invariantlab/main.go

# schema gate lab: a producer/consumer refuses to Register when the db's system
# or topic schema version is outside the range this build understands -- fail
# fast with an operator-actionable message. The v1.1 upgrade path's tripwire.
schema-gate-lab:
  go run examples/phase_1/schemagatelab/main.go

# producer batch lab: the batched payload-only Produce path. Concurrent
# callers share transactions (xmin-proven) and land exactly once, a
# caller-keyed call routes per-call and dedups, a poisoned/unencodable
# payload fails only its own caller, hot compaction keys never deadlock
# across concurrent batches, bursts self-heal missing partitions, and a
# timing pass (batched vs per-call at equal concurrency, plus a saturated
# batched arm) reports what the fsync amortization actually buys in-library.
producer-batch-lab:
  go run examples/phase_1/producerbatchlab/main.go

# create-ahead lab: every append path (per-call, batched, in-tx) creates the
# next partition at the 80% trigger point -- polled into existence before the
# boundary, zero heal warns, ids contiguous (no burned boundary id), exactly
# one partition ahead.
create-ahead-lab:
  go run examples/phase_1/createaheadlab/main.go

# worker claim lab: N consumers on one topic coordinate through worker claims --
# target-1 rows (janitor, waterline) hold exactly one live instance (not N),
# failover to a survivor within a reconcile tick when consumers die, and full
# release when the last exits.
worker-claim-lab:
  go run examples/phase_1/workerclaimlab/main.go

# Phase 14a chunk 7 lab: the end-to-end bridge pattern proof -- a user-space
# consumer group transforms+re-produces v1's compacted winners into a newly
# registered v2 at CompactionRank -1 while live producers write straight to
# v2 at rank 0. Confirms zero-pause (live always beats the bridge, either
# arrival order), a crashed-and-restarted bridge resumes from its cursor with
# no duplicate rows, and that drain telegraphing never calls a compacted
# topic safe on its own even once this lab proves it actually is -- that
# stays an operator call. Registers both versions, destroys both on exit.
schema-evolution-lab:
  go run examples/phase_1/schemaevolutionlab/main.go

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