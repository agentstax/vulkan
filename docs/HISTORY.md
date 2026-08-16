# History

Dated ledger of what shipped, newest first — one entry per milestone.
`[NNNN]` cites the decision record `docs/decisions/NNNN-*.md` holding the why.
Entries before 2026-08-13 were reconstructed from the phase notes when this
ledger was created; dates come from the phase git tags.

## 2026-08-15 — Config becomes code-owned [0518] [0520] [0521]

- Config is declared in code and the latest declaration wins. RegisterTopic,
  a consumer/worker Declare and RegisterCronJob each write their declared
  mutable config onto the row they find, logging old -> new at Info when it
  replaces something different (`replaceTopicConfig`, `replaceCronJobConfig`,
  each in its datastore's `replace.go`).
- Deleted: AlterTopic, AlterGroup, AlterWorker(s), AlterCronJob, their
  Alter*Config/Alter*Data types and to* adapters, UpdateTopic, UpdateCronJob,
  MetadataValue with applyOverrides/mergeMetadata/declaresKey,
  pkg/common/update.go, ErrCronJobConfigMismatch, and `topic config
  set|unset` / `group config set|unset`. Both `config get`s stay.
- Worker metadata is the plain typed value per key -- each kind's metadata
  struct holds a `time.Duration` / `int` / `common.MessageOptions` directly,
  and the stored JSONB flattens from `{"poll_rate":{"default":N}}` to
  `{"poll_rate":N}`. [0516]'s repeat_interval lands as one of those fields.
- Identity and action state are not config: partition_size still raises
  ErrTopicConfigMismatch, a cron job's owner columns are written at creation
  only, and both `suspended` and `target_instances = 0` survive a
  redeclaration -- SuspendCronJob/UnsuspendCronJob are what change the former.
- The CLI creates nothing ([0521]): `vulkan topic register` and `vulkan cron
  register` are deleted, matching `vulkan system`, which never had one, and
  cmd/vulkan/README.md says where topics and cron jobs come from instead.
- New surface in the same sweep ([0520]): a JobConfig (Schedule, Threshold)
  per built-in alert, composed into admin.RegisterSystemConfig, which
  RegisterSystem now takes; ensureSystemCronJob and ensureSystemTopic
  collapsed into ordinary register calls.
- Labs: the producer/consumer stand-ins (consumer, bench, variance, crashlab)
  resolve their topic with GetTopic and exit with a clear message when it
  isn't registered; registeridempotencylab, idempotencykeyslab, cronlab,
  alertlab and reservedtopiclab assert the newest declaration wins; topiclab's
  partition proofs wait for create-ahead's partition instead of racing it.

## 2026-08-15 — Destroy system [0514]

- `admin.DestroySystem` completes the destroy-verb set (topic, group,
  system): RegisterSystem's inverse, deleting every registered topic through
  the existing `DeleteTopic` path, then dropping the shared control-plane
  tables in one transaction under the register's own advisory lock.
- Guards unless Force: any live worker instance -> `system.ErrSystemLive`;
  any non-`__system.` topic -> `system.ErrTopicsRegistered` (Force takes
  user topics and their messages too).
- `vulkan system destroy` with --force/--yes; the confirmation phrase is the
  connected database's name (`current_database()`).
- destroysystemlab covers both guards (worker guard outranks topic guard),
  the clean teardown of all 13 tables, and re-registering afterward.

## 2026-08-14 — Producer proactive partition create-ahead [0512] [0513]

- Append paths create the next partition early: an appended id (or batch
  range) landing on a partition's trigger point (80%, 95% backstop —
  `CreateAheadGate`) wins a per-topic monotonic CAS claim and runs
  `ensureCoveringPartition` in a detached goroutine. Best-effort by design:
  warn-and-drop, the boundary heal stays the only correctness layer. One id
  sequence means exactly one append fleet-wide sees each trigger id — zero
  coordination.
- The heal path's thundering herd is capped: a blocking
  `pg_advisory_xact_lock` under the existing 2s lock_timeout means one
  winner runs the CREATE and losers wake after its commit to a no-op.
- The detached run's timeout derives from the retry policy (new
  `Policy.CalculateTotalDelay` + per-attempt allowance); lock_timeout
  expiries reclassify as retryable on this path only; a destroyed topic
  evicts its gate entry on undefined_table. `TopicConfig.Validate` gained a
  `PartitionSize >= 2` floor.
- createaheadlab proves all three append paths create ahead of the boundary
  (partition exists before it, zero heal warns, contiguous ids);
  partitionlab reshaped onto deterministic create-ahead polling.

## 2026-08-13 — Binding lifecycle: sets declared at consumer Register [0511]

- `Consumer.Register(ctx, group, topic, version, bindings)` states the
  group's full binding set (nil = whole topic). Consume re-attempts the
  declaration until installed or joined before starting the manager; a
  waiting outcome (a live instance still declares a different set) retries
  every `ConsumerConfig.BindingRetryInterval` with a Warn per attempt,
  forever — never fencing the incumbent.
- Storage is the append-only `binding_declaration` table, one row per
  attempt: effective set = the group's newest installed row, a declarer's
  newest waiting row is its retry heartbeat; `declared_at` (episode start)
  + `attempt_at` per row; concurrent installers serialize on the
  consumer_group row lock; claims keep reading `binding` rows, swapped only
  inside the install transaction. Declarer identity is
  `common.ProcessIdentity` (hostname:pid:random, once per process).
- The create-only path is gone: ConsumerController.Bind/ClearBindings and
  their datastore pairs deleted. Alert `Declare` states {JobName} through
  DeclareBindings at RegisterSystem — an undeclared group reads as
  whole-topic, which had `cron get --requests`-style listings matching every
  job.
- Read surface: `MessageAdmin.ListDeclarations` returns
  `binding.Declaration` rows (each group's effective declaration plus open
  waiters); `vulkan alert bindings` shows status/patterns/declarer/
  timestamps. The per-pattern ListBindings listing was deleted.
- New bindinglab (`just binding-lab`): same-set join, divergent wait against
  a live incumbent, dead-fleet swap ending in consumption under the new
  set. Six labs moved from Bind onto DeclareBindings. 40/40 fresh-DB suite.

## 2026-08-13 — Lab binaries build into bin/

- `just build-lab <lab>` compiles a lab's main.go to `bin/<lab>`; `bin/*` is
  gitignored except `.gitkeep`.
- The per-binary .gitignore entries (`reclaimlab`, `routinglab`) and the two
  stray compiled binaries at repo root were removed; the bench projector
  binary followed, and .gitignore's enumerated bench/idempotency scratch list
  collapsed to `bench/idempotency/*` + `!RESULTS.md`.
- conventions.md renamed CONVENTIONS.md to match the doc naming pattern.

## 2026-08-13 — Record-keeping surface redesign

- LEARNING_PLAN.md/TODO.md/NOTES.md reorganized into docs/: ROADMAP.md
  (future, Now/Next/Later/Parking lot), TODO.md (in-flight sliding window
  only), this ledger, and per-decision records in docs/decisions/ with
  docs/DECISIONS.md as the retrieval index. TEST.md moved along with them;
  only the rule files stay at repo root.
- The decision history was distilled out of the phase notes: ~250 records
  covering phases 1 through 14a.
- LEARNING_PLAN.md and NOTES.md deleted (full files in git history) after
  archiving the user's Explain-it-back answers verbatim to
  docs/archive/explain-it-back.md — both the NOTES.md sections and the 42
  answers written inside LEARNING_PLAN.md itself.

## 2026-08-13 — 14a (alerts): default alert checks & the 14a gate

- Default checks (`partition_count`, `compaction_read_cost`) shipped as cron
  jobs with per-check worker-kind subpackages; one consumer group per check
  bound to its exact job name, the central dispatcher killed for good
  [0481][0482].
- The compacted `__system.alerts` topic is the state store, dedup memory,
  integration surface, and — via the repeat republish — its own retention
  keepalive [0483].
- Transitions decided by the pure `classify(found, head, repeat, now)`;
  evidence rides the alert but never enters the decision [0484].
- No notifier component: WARN/INFO edges are a side effect of
  `AlertController.Record` comparing the publish against the head, making the
  pipeline restart-proof and idempotent [0485][0488].
- Executors are worker definitions the manager claims (goroutine hosting
  rebuilt away); each check's Evaluate condition lives once in its controller
  and is injected into producer and consumer [0486][0487].
- Checks self-declare at `RegisterSystem` with schema-level idempotent binds;
  the register-time evaluator pass is log-only, leaving `Record` the single
  writer to alert state [0489][0490].
- Gate swept same day: `MessageAdmin.DestroyGroup` + `vulkan group destroy`,
  `vulkan alert bindings`, GroupLag.ParkedExceptions renamed
  UnresolvedExceptions, 35/35 fresh-DB labs green, `git tag phase-14a`.

## 2026-08-12 — 14a (cron): cron_job scheduler & job_requests

- Scheduled work built on the existing messaging machinery: `cron_job` spec
  rows, a `cronscheduler` worker producing per-job requests onto one
  compacted `__system.job_requests` topic, consumers binding job names as
  routing keys [0461][0462].
- Concurrency stamped into MessageOptions and enforced only at consume time
  by the key lease; the scheduler drops missed times, walking to the newest
  due time per job [0463][0464].
- Job-request status is fully derived — no status column — from message_log,
  compaction_head, and delivery_log in mode 'all', with terminal outcomes
  classified before not-head "superseded" [0465][0466][0467].
- Registry verbs (idempotent register, altering re-seeds the due time, gated
  destroy) with a vendored robfig schedule core; the scheduler commits one
  transaction per row so a poisoned job cannot stall its siblings
  [0468][0469][0470].
- `RunCronJob` takes a fresh v7 idempotency key per call and defaults to
  'allow'; "firing" retired from the vocabulary codebase-wide [0471][0472].
- Status/listing datastore reads decomposed from one CTE statement into flat
  per-fact queries composed in Go, deliberately trading away single-snapshot
  consistency [0473].

## 2026-08-08 — 14a (worker system): worker/worker_instance & the manager

- One generic worker/worker_instance pair replaced all per-feature
  maintenance/duty plumbing: a worker row is the spec, an instance row is a
  heartbeat-held lease, and suspend is target_instances = 0 [0421];
  exclusivity moved from claim-per-tick to claim-per-instance, trading
  failover latency for once-per-lifetime arbitration [0422].
- The manager runs one reconcile loop, respawning only on ErrInstanceLost and
  propagating every other error [0423]; the consumer inversion made the
  consumption loops ordinary worker rows and the public Consumer a seeding
  construct over a manager runner [0424], with self-claimed loops marked
  NoInstanceTarget (-1) [0430].
- The daemon (`vulkan manager run`) is the same manager claiming one system
  manager row; deployment scope is a single WHERE clause and topic manager
  rows were deleted [0425]; the umbrella package was named systemmanager
  after researching comparable systems [0431].
- The producer split into pure factory + stateless Register(ctx, topic,
  version); the stored lifecycleCtx and its shutdown error family were
  deleted [0426].
- WorkerSnapshot/CronJobSnapshot verdicts became classify functions with a
  flat 10m overdue threshold [0427].
- The migration was built beside pkg/maintain and switched over, never
  hybridized [0429]; the final cleanup deleted EnsureNextPartition without a
  rehome — partition creation belongs to the write path, the janitor is
  cleanup only [0428].

## 2026-08-06 — 14a (schema evolution): epoch-versioned topics

- A breaking message-schema change is now a new physical topic under the same
  name — `topic (name, schema_version)` UNIQUE, all lifecycle verbs
  version-addressed, `SchemaVersion` a required positional constructor param,
  and no unversioned `GetTopic` [0401][0405][0409].
- Compaction's winner rule generalized to a signed caller-supplied rank
  compared before id — `CompactionRank` on `message_log` and
  `compaction_head` with a native row-compare guard — so a bridge writing at
  rank −1 can never overwrite a live rank-0 write [0403].
- Migrating a compacted topic is deliberately user-space: the bridge consumer
  pattern, built on `consumer.MetaFromContext` message metadata and
  source-id-derived idempotency keys for crash-safe resume [0402][0404][0407].
- `FamilyHealth` telegraphs when an old version can retire but never calls a
  compacted topic safe; it lives in `pkg/admin` as `DestroyTopic`'s own
  question, a built `pkg/metrics.HealthMetrics` having been reverted
  [0406][0411].
- Naming settled: the catalog column stays `schema_version` despite the
  `schema_log` overlap, and topic identity in logs/metrics stays two
  structured fields, never `name@vN` [0408][0410].

## 2026-08-04 — 14a (consumer layering): the layered package pattern

- The three-layer shape (pkg/<x> vocabulary, controller as the only door,
  table-exact datastore, import arrows strictly down) became the house
  standard and was rolled across worker, topic, system, consumer, metrics,
  and cron [0441], with all input validation at the controller and trusting
  datastores [0442].
- pkg/consumer converted around one non-negotiable symbol:
  consumer.NewConsumer stays put, so pkg/consumer is a door with read-models
  in the controller, sorted by "nothing a user types lives below the door"
  [0443]; the consumption loops became packages with narrow configs,
  duplication accepted [0444].
- MessageMeta forced the single new leaf pkg/consumer/message, because its
  unexported ctx key cannot be duplicated per package [0445].
- Cleanups carried with the conversion: the phantom ConsumerDatastore type
  parameter deleted [0446]; uuid.UUID above the datastore, pgtype.UUID below,
  ErrLeaseLost declared at its detection site [0447].
- No shared base package at first (~150 lines copied per loop) — since
  superseded by pkg/consumer/base [0448]; deliveryconsumer was kept unrun
  rather than deleted [0449]; rangeState/claimBuffer stayed private to
  protect atomic write ordering [0450]; the house name stutter stays until
  the v1 API review [0451].

## 2026-08-01 — Phase 14: v1 hardening & correctness fixes

- `topic.Destroy` no longer exhausts Postgres's shared lock table: partitions
  drain in fixed 100-per-transaction batches of plain `DROP TABLE IF EXISTS`
  (no DETACH), crash-resumable and bounded against live producers
  [0381][0382][0383][0384].
- Two live cursor-claim races proven and closed: a READ
  COMMITTED/EvalPlanQual double-delivery race via `FOR UPDATE` on the
  old_values read, and permanent message loss from late-committing producers
  via the snapshot fence — claims stop at a proven head, with unconditional
  pending-pair storage and an idle short-circuit [0388][0394][0395][0396]; a
  missing cursor row now errors loudly instead of reading as "caught up," via
  a query restructure that also came out 25-30% faster at high partition
  counts [0387].
- FanOut moved from full-log rescans to a hardened per-group mark on the
  cursor table: LIFECYCLE groups register cursor rows and pin retention,
  delivery runs eagerly past the proven head, and binding changes are
  forward-only [0389][0390][0391][0392][0393].
- The abandoned-routines map stays deliberately unbounded; its real leak — an
  Add/Remove ordering race — was fixed structurally with a reaper goroutine
  [0385][0386].
- Written decisions closing standing questions: no deliveries status index
  for v1 (reopen only on measured evidence, partial index preferred) [0397];
  no DELETE CASCADEs or triggers — visible DML over schema-resident behavior
  [0398]; `Process` claim errors stay per-tick fatal [0399].
- SQLSTATE retry classification hardened: connection-death and
  ambiguous-commit codes became retryable after a ~21-site retry-safety
  audit, with resource-exhaustion, corruption, and misconfiguration codes
  explicitly excluded [0400].

## 2026-08-01 — Phase 13: public API design review (v1 gate)

- Painstaking pre-v1 pass over everything exported from producer, consumer,
  and topic: every item got an explicit written decision, with anything
  API-shape-affecting pulled forward before the shape froze.
- Producers and consumers got a shared registration lifecycle: Register(ctx)
  with fail-fast topic-handle validation [0363], once-per-instance
  registration and shared sentinels [0365], merged session ctxs with
  nil-on-wind-down [0366], and a deleted shutdown hook in favor of app-owned
  datastore Close [0368] — the producer's ctx capture itself was later
  dropped [0361].
- Vocabulary and placement settled: the generic became Message with cascading
  type renames [0369], table-name plumbing moved to internal/topic [0371],
  janitor/partition tuning became persisted topic-row state [0372], and
  QueueTimeout became QueueMargin [0373].
- Retry got one mental model — retry.Policy carried per config [0374], reused
  for exception backoff [0375] — and validation moved to construction via the
  library-wide WithDefaults-then-Validate convention [0377].
- PartitionSafetyBuffer was deleted for unconditional one-ahead partition
  creation [0378] with multi-partition premake explicitly rejected [0379];
  the dead datastore interfaces were collapsed to concrete types [0380];
  migrations became admin.MigrateTopic/MigrateSystem calls [0501].
- The circuit breaker's full two-tier design was settled at feature level for
  a later build [0502-0506], and the public surface was trimmed to three
  audiences with concurrency, low-level retry, and ConsumerType demoted
  [0507-0510].

## 2026-07-20 — Phase 11.5: admin surface, migrations-into-code & CLI
(built incrementally across July 2026; no phase tag [0356])

- pkg/migrate: schema as versioned Go with runner-owned transaction
  boundaries; golang-migrate deleted, RegisterSystem's idempotent baseline is
  bootstrap [0341][0342].
- schema_log with latest-by-id current-version rule, independent system/topic
  version counters, and one advisory lock with session- vs xact-scoped
  hold-times [0343][0344][0345].
- Explicit migration registries with contiguity tests plus teaching-error
  gates (ErrNotRegistered, AssertSchemaSupported, translateAdminError)
  [0346][0347].
- pkg/admin.MessageAdmin: register/get/list/alter/rename/destroy + migrate
  verbs; AlterTopic as a pointer patch over a static COALESCE UPDATE,
  PartitionSize immutable by omission, id-keyed rename as its own verb
  [0348][0349][0350][0351][0352].
- delivery_log_<id> now always created, DisableDeliveryLog gates writes only
  [0353].
- cmd/vulkan as a nested-module CLI (cobra/fang/lipgloss, gitignored go.work)
  with sparse flags mapped via Flags().Changed; topic and migrate command
  trees [0354][0355].

## 2026-07-16 — Phase 11: architecture cleanup — datastore boundary & producer API

- Multi-target transactional enqueue: producer.InTransaction +
  WorkProducer.ProduceInTx, per-target SAVEPOINT partition self-heal with no
  opt-out, no auto-retry [0321][0322][0323].
- Attempt audit trail: per-topic delivery_log recording failed attempts only,
  written in the same transaction as the delivery mutation; deliveries made
  per-topic and the six shared tables singularized first [0324][0325].
- context.WithTimeoutCause at all three nested-timeout sites, naming which
  budget fired [0326].
- Commit/PartialCommit exception writes collapsed to one pgx.Batch — 3.16ms
  at N=1 to 9.3ms at N=1000; RecordException left unbatched for durability
  [0327].
- Datastore boundary audit: one pgtype.UUID leak, constructors found to
  bypass the interface entirely; keep/simplify/remove deferred to a standing
  cleanup phase [0328].

## 2026-07-14 — Phase 10: observability — logging, queue-state metrics & OTel

- Pluggable logger interface with an io.Writer-backed default implementation
  [0304].
- One queue-state datastore query per (group, topic) — backlog,
  claimed-committed inflight gap, ready/inflight/dead exception counts,
  oldest-unacked age, open leases — generalizing the ad hoc `just lag`
  recipe [0305].
- Metrics snapshot merging that query with the in-process AbandonedRoutines
  counters; the debug readout and OTel instruments both read only the
  snapshot [0303].
- OTel integration on the metric API package only, no-op provider by default;
  all 13 instruments verified on a scraped Prometheus body in otel-export-lab
  [0302].
- Lazy-vs-synchronous waterline rollup measured and resolved: stayed lazy
  (synchronous was 1.3x-1.9x slower at 20 committers), added
  WaterlinePollRate [0301].
- Three labs: metrics-reaction-lab (each failure shape moves exactly its
  numbers), metrics-load-lab (15.6x catch-up cut at a fast poll rate),
  otel-export-lab.

## 2026-07-12 — Phase 9: consumer fault isolation & recovery

- Panics, hangs, and datastore blips in `consumerFunc` now all land in the
  same per-message retry/backoff/dead-letter path as an ordinary error — one
  message affected, never the whole claimed range [0281].
- One shared `callSafely` wraps `consumerFunc` for all three claim paths:
  `recover()` inside the spawned goroutine converts panics to errors sent on
  `done`, raced against `WorkTimeout + WorkTimeoutGrace` (default 100ms,
  sized from measured p99 <1ms scheduler wakeup) [0287][0288][0289][0291].
- Abandoned timed-out goroutines are tracked in a mutex-guarded registry
  keyed by `(MessageId, Attempt)`, kept as plain in-process state ahead of
  any metrics-library commitment [0292][0293].
- `pkg/retry` (explicit retryable/permanent classification, public/private
  datastore split) made blips survivable; the `idempotency_key` claim table
  closed the resulting double-publish-on-ambiguous-ack gap, its measured cost
  cut with a batched CTE plus `SkipIdempotency`, which in turn moved
  `Commit()` classification inline at call sites [0282][0283][0284][0285].
- Graceful shutdown narrows an interrupted lease via `PartialCommit` so the
  unprocessed suffix rides the existing crash-recovery reclaim path [0286].
- The fault-isolation lab caught a live `retry.Wrap` bug —
  success-after-retries returned as a joined error — fixed with a plain nil
  early-return [0294].

## 2026-07-11 — 8c: log compaction, latest-per-key filtered at claim time

- Compacted topics ship with an append-only log: superseded rows stay
  physically present and `readMessages`/`FanOut` filter to the current latest
  row per `compaction_key`, so retention is disk cleanup, never a correctness
  gate [0261].
- The filter predicate is unbounded — re-checked live on every read including
  reclaims — after a crash/reclaim race proved a claim-high-bounded check
  could drop a superseded key's delivery entirely [0263].
- `latest_key(topic_id, compaction_key, latest_id)`, one shared table
  upserted in the producing transaction with an id-value guard, replaced the
  correlated scan with an O(1) lookup; the old scan and its partial index
  were deleted outright [0262][0267][0268][0272].
- Both sides of the tradeoff were measured, not assumed: the scan is linear
  at ~10µs/partition (extrapolating to ~1s per surviving key at 100K
  partitions), the upsert is noise uncontended and 2.5-2.9x slower on a
  single hot key [0271][0273].
- Retention janitors stay compaction-unaware — a dormant key aging out is
  intentional — and garbage-collect the dangling `latest_key` row via each
  function's existing deliveries-cleanup pattern [0269].
- No schema-level tombstone (deletion lives in the payload) and no history
  backfill (built, verified live, reverted — no deployment predates the
  table) [0266][0270].

## 2026-07-10 — 8b: per-topic tables

- `message_log` split into per-topic `message_log_<topic_id>` tables with
  dense per-topic sequences; `RetentionTTL`/`AllowDropPastCommitted` moved
  onto `topic.Config`; the global table and dead V1 consumer datastore
  package deleted [0241].
- `cursor`/`deliveries` PKs and `binding` gained `topic_id`; `lease` got the
  column without a key change; `cursorFloor` scoped `WHERE topic_id = $1`,
  closing 8a's cross-topic floor bug [0243][0241].
- Topic identity threaded as a `topicID` parameter on every datastore method
  after a mid-phase reversal away from a struct field; constructors split
  into `NewConsumerDatastore`/`NewProducerDatastore` [0244].
- `routing_key`/`bindings` kept above topics with matching logic unchanged;
  within-topic floor sharing across slices left as a deliberate limitation
  with topic-splitting as the escape hatch [0242][0248].
- `deliveries` stays one shared table; a `status` index was considered and
  deliberately not added to preserve HOT updates [0246][0247].
- All 11 labs plus the producer/consumer binaries rewritten to the current
  API; new `topiclab` proves sequence independence, floor isolation, and
  clean `42P01` failure on unregistered topics [0245]; two latent cross-topic
  reclaim/cursor-advance bugs found and fixed.

## 2026-07-08 — 8a: retention — partition-drop plus a low-volume sweep

- `message_log` converted to `PARTITION BY RANGE (id)` in its original
  migration, width via `WorkConsumerConfig.PartitionSize` [0221].
- `WorkConsumer.Janitor` ticker loop runs create-ahead,
  `DropExpiredPartitions`, and batched `SweepExpiredPartitions` each tick;
  drop covers real volume, sweep covers the low-volume gap [0222].
- `RetentionTTL` (zero disables) and `AllowDropPastCommitted` (default false)
  added; drops and sweeps stop at the `MIN(committed)` cursor floor unless
  opted out [0223].
- Bug fixed: `Register` upserted a cursor row for LIFECYCLE groups, whose
  `committed` never advances, permanently pinning the drop floor at 0 — now
  gated to CURSOR-type groups [0225].
- Three new labs (`partitionlab`, `dropfloorlab`, `sweeplab`) drive the real
  datastore methods; the first two swap `message_log` to a lab-scale
  partition width and restore the schema on exit [0224].
- Known limitation filed: one shared log means one lagging group blocks drops
  for every routing key sharing the table [0226].

## 2026-07-03 — Phase 7: routing_key + binding-based routing

- `message_log.routing_key` and `binding(consumer_group, pattern, display)`;
  a group with no bindings still receives everything, so all prior behavior
  is unchanged by default [0201].
- One shared predicate in both paths: `readMessages` filters returned rows
  but the cursor advances the whole claimed range; `FanOut`'s SELECT never
  materializes non-matching `deliveries` rows; evaluated at claim/fan-out
  time, so late-added bindings apply to still-unclaimed messages [0202].
- Patterns are true wildcards translated to anchored POSIX regexes;
  NATS-style depth precision deferred [0203].
- `BindTopic`/`ClearBindings` shipped as admin calls off the `Datastore`
  interface [0204].
- Found and fixed a latent bug: `readMessages` used `SELECT *`, so the cursor
  read path had been silently broken since `routing_key` landed — explicit
  column lists from here on [0205].
- `routinglab` (self-seeding, three groups) proves depth-crossing,
  retroactive bindings, and both paths' filtering; `kind`/`header_match`
  dropped, `FanOut`'s full-table rescan logged as a known separate limitation
  [0206][0207][0208].

## 2026-07-03 — 6.5c: per-message exception handling for failing messages only

- `Commit(group, token, []MessageException, []MessageTerminal)`: frees the
  lease first (`ErrLeaseLost` when stale), then records one `deliveries` row
  per failure — `ready` for retryable, `dead` for terminal — so one bad
  message no longer fails its batch-mates [0181][0182][0186].
- Statuses collapsed to `ready | inflight | dead`, no `done`: success writes
  nothing, and `RecordExceptionSuccess` deletes a resolved row [0181][0188].
- `DrainExceptions` added as its own poll loop; `ClaimExceptions`
  dead-letters expired-inflight rows at `maxAttempts` before claiming
  [0184][0185].
- `AdvanceWaterline` gained a second blocker term — `committed` pins below
  the lowest unresolved `ready`/`inflight` exception; `dead` rows don't block
  [0183].
- Reclaim rewritten as one atomic UPDATE, fixing a `reclaims`-counter reset
  bug; past `MaxRangeReclaims` a range's messages all become fresh-budget
  `ready` rows and the lease is freed for good [0190][0191].
- `exceptionlab`: proves `committed` pins below one recorded exception while
  later ranges commit past it, then jumps to `claimed` on resolution.

## 2026-06-30 — 6.5b: lease-per-range crash recovery for the cursor path

- New `lease(token, consumer_group, low, high, until)` table; `ClaimMessages`
  inserts the lease in the same transaction as the `claimed` advance and
  returns `ClaimedRange{Lease, Messages}` [0161][0163].
- `ClaimMessagesWithCursor` reclaims one expired lease before any fresh
  claim, re-reading the exact `(low, high]` range under a fresh token [0164].
- `CommitRange(group, token)`: token-guarded lease delete replaces
  per-message `MoveCursor`; all waterline motion moved to the `RollWaterline`
  goroutine off the hot path [0166].
- `AdvanceWaterline` split into a one-snapshot SELECT plus a `GREATEST`
  UPDATE after a live multi-worker run exposed the single-statement
  EvalPlanQual bug [0162].
- `reclaimlab`: deterministic crash/reclaim verification — exact-range
  re-read, token rotation, waterline pin, `deliveries` stays empty; cap on
  repeated reclaims deferred as a named handoff [0165].

## 2026-06-26 — 6.5a: claim-from-log happy path

- `cursor` grew `position` into `claimed` + `committed` frontiers;
  `ClaimMessagesWithCursor` advances `claimed` over `(low, high]` in one
  `UPDATE … RETURNING` and N successes collapse into advancing two integers
  on one row [0141].
- The pre-update `claimed` is captured via an `old_values` CTE joined back in
  `FROM`, avoiding PG18's `old` alias so the query runs on PG <18 [0144].
- `MoveCursor` landed the long-forecast monotonic guard
  `committed = $1 WHERE committed < $1`, at the cost of an ambiguous
  `RowsAffected()==0` [0143].
- Per-message commit kept over once-at-`high`, trading batch-level O(1) for a
  tighter crash checkpoint [0142].
- No lease yet: a crash between claim and commit strands
  `(committed, claimed]` — the known hole the range-lease work closes [0145].

## 2026-06-25 — Phase 6: per-row synthesis measured (approximate date; no tag)

- The lifecycle × fan-out synthesis: a `deliveries` row per (group, event)
  gives per-message state under fan-out.
- Its write-amplification wall was measured and became the motivation for the
  claim-from-log refactor that followed.

## 2026-06-23 — Phase 5: fan-out to independent consumer groups over one log

- A `-group` flag plus `just consume group=…` runs multiple groups side by
  side, each an independent `cursor` row over the shared `message_log`;
  `Register` upserts a new group's cursor at position 0 so it replays
  retained history [0121].
- `Process` became a real poll loop (`time.Ticker` on `PollRate`,
  `ctx.Done()` to stop) with `Claim` as the per-batch body [0122]; the loop
  idles one interval before its first claim [0126].
- `just lag` reports `head − position` per group; the lab showed a slowed
  group's lag climbing while another stayed near 0.
- Kept `message_log`/`cursor`/`position` naming over the plan's
  events/consumers vocabulary [0123]; corrected the Phase 4 forecast — the
  monotonic guard belongs to within-group concurrency, not fan-out [0124].
- Failure semantics unchanged: a `consumerFunc` error stops the poll loop;
  retry/DLQ deferred to the `deliveries` work [0125].

## 2026-06-23 — Phase 4: log/queue split lands retention and replay

- Split messaging into append-only `message_log` (`id BIGSERIAL`, `payload`,
  `created_at`) and per-group `cursor` (`consumer_group`, `position`); the
  lifecycle columns and their migrations left the hot path [0101].
- `ClaimMessagesV2` reads the range above `position` in one transaction,
  draining rows before commit to avoid pgx "conn busy" [0107]; `ProcessV2`
  advances the cursor per message [0103].
- A missing `ORDER BY id` in the first claim cut could silently drop offsets
  forever; the fix established that a high-water mark is only correct over an
  ordered claim [0102].
- `MoveCursor` stayed an unguarded `SET position = $1`, safe under one
  consumer per group [0104]; claim-time `FOR UPDATE` serializes claims only
  [0105].
- V1 lease/backoff/`Record*` code kept as reference rather than deleted
  [0106]. Lab confirmed a fresh group at position 0 replays history
  independently.

## 2026-06-20 — Phase 3.5: the commit wall measured

- Only `synchronous_commit` measured, the one lever blind to the upcoming
  topology change; batch-ack measurement skipped since the cursor model is
  its limit case [0081].
- on/off sweep at batch=100: 5.99× at 1 worker shrinking to 1.28× at 64 as
  group commit amortizes concurrent fsyncs under `on`; ~484µs fsync wait at
  conc=1; read by shape with best-of-3/max [0084].
- `off` ruled safe for this queue: a lost commit becomes lease expiry →
  reclaim → rerun, risk already priced by at-least-once plus idempotency
  [0082].
- Crash lab (SIGKILL mid-run, wal_writer_delay widened to 10s): 5000/5000 ids
  processed and `done` under both settings; `off` cost 899 vs 85 duplicate
  reruns — duplicates, never loss [0083].
- `local` left unmeasured (identical to `on` without a replica) [0085]; the
  knob was applied via ALTER DATABASE so pool connections inherit it, then
  reset to `on` [0086].

## 2026-06-20 — Phase 3: competing consumers & batching

- Built the Prefetch/Dispatch pipeline: batch claim into a bounded
  PressureQueue, one goroutine per message under WorkerPoolLimiter,
  backpressure via WaitForRoom [0061]; buffer kept shallow as a lease-safety
  rule [0062]; results recorded per message so a batch is never a failure
  unit [0063].
- Index lab: the ORDER BY id claim degraded 0.057ms → 41.8ms (730×) against
  150k terminal rows; migration 005's
  `idx_claim_active (id) WHERE status IN ('ready','processing')` recovered
  ~0.09ms and ~4.8k → ~19k msgs/s on a deep backlog [0065].
- Ceiling lab settled the model
  throughput = min(supply(batch), ack_capacity(workers)): single-loop supply
  ~290k/s, per-message commit wall ~20-22k msgs/s at 64 workers [0064]; pool
  MaxConns raised past the default 10 to make worker sweeps meaningful
  [0066].
- Group-commit valley observed: 2 workers slower than 1 (~1.1k vs ~1.7k/s)
  until group commit amortizes from 4 workers up.
- Variance proof: 3 slow messages at the head of 6000 fast never stalled fast
  throughput at 8 workers (wall 6.1s vs 60.7s at concurrency=1).
- Future scaling levers fixed in priority order [0067].

## 2026-06-15 — Phase 2: per-message lifecycle

- Migration `003_lifecycle` adds `status`, `attempts`, `can_run_after`,
  `locked_at`, `last_error`; claiming flips `ready` → `processing` instead of
  deleting [0041].
- Claim is one `UPDATE ... RETURNING` with `FOR UPDATE SKIP LOCKED` in the
  subquery; crashed-worker reclamation is an `OR` branch on the same
  predicate — no reaper [0041][0044].
- Retries back off via `can_run_after`; exhausted messages land in `dead`,
  and the DLQ is the query `WHERE status='dead'` [0042][0043].
- Stuck window set to work timeout + 5s so live workers are never reclaimed
  [0045].
- Consumer becomes sole owner of `BatchLimit`/`MaxAttempts`/`WorkTimeout`,
  killing the silent-no-op duplicated datastore config [0046].
- Double-delivery induced deliberately (sleep past the lease); at-least-once
  with idempotent `consumerFunc` adopted as the contract [0047].

## 2026-06-14 — Phase 1.5: transactional enqueue

- `AppendMessage` opens the transaction, runs the caller's
  `ProducerFunc(ctx, tx)` on it, then INSERTs into `message_log` and
  commits — both writes land or neither [0021][0022].
- `ProducerFunc` takes a concrete `pgx.Tx`, with a marked extraction point if
  a second backend appears [0023].
- Migration `002_users` adds the toy business table for the labs.
- Labs pass: forced rollback leaves neither row; commit yields both plus a
  claimable job; a consumer never sees an uncommitted producer's message
  [0021].

## 2026-06-13 — Phase 1: durable atom — append plus atomic claim

- Claim → process → delete → commit as one transaction; the claim is
  `SELECT ... FOR UPDATE SKIP LOCKED` [0001][0002].
- Crash recovery is transaction rollback — kill-mid-process and crash-after
  labs pass with zero recovery code [0002].
- Two-workers-no-collisions and blocking-vs-skipping contrast labs pass
  [0001].
- Batch limit pinned to 1 to avoid batch poisoning [0004].
- Graceful shutdown finishes the in-flight batch via `context.WithoutCancel`
  + timeout; a graceful stop and a crash look identical to the queue [0005].
- Table name stays `message_log`; the `jobs` rename is deferred to the
  log/queue split [0006].
