# History

Dated ledger of what shipped, newest first — one entry per milestone.
`[NNNN]` cites the decision record `docs/decisions/NNNN-*.md` holding the why.
Entries before 2026-08-13 were reconstructed from the phase notes when this
ledger was created; dates come from the phase git tags.

## 2026-08-22 — cross-version compatibility: MinCompatibleVersion gate + compat lab [0580]

- Every migration step now declares MinCompatibleVersion (0 = additive,
  own version = breaking; empty steps bump the version for
  compatibility-only releases), stored per migration_log row in the v1.0
  baseline DDL. The schema gate reads both facts in one query and admits
  a build iff `min_compatible_version <= build <= current` — additive
  skew is the rolling-deploy window, a breaking step past the build
  refuses at Register (VK0023 attrs now min_compatible_version +
  build_version). Build versions derive from the registries
  (`len(Registry) + 1`); the four Min/Max constants are gone.
  schemagatelab reshaped (additive window, breaking refusal, per-topic
  skew via a sibling topic); tools/compat nested module + `just
  compat-lab` dry-run green (pins the working tree until two releases
  exist); CONVENTIONS ## Migrations release-era rules, website
  guides/migrations.mdx with the compatibility table, and the AGENTS.md
  release checklist landed in the same change.

## 2026-08-22 — fillfactor audit closed: adopt nothing [0578]

- Static pass classified every table's update paths (cursor /
  compaction_head / delivery the real candidates; lease, key_lease,
  worker_instance, cron_job ruled out structurally), then live
  benchmarks confirmed all three were already ~100% HOT at default
  fillfactor — throughput identical within noise, [0574]'s 36k dead
  tuples revealed as HOT-chain tuples pruned in place. Baseline DDL
  untouched.
- New bench/fillfactor consume-side harness (pre-fill a fresh topic,
  drain through real ConsumerInstance.Consume calls; failure-rate flag
  cycles the exception window for delivery churn; per-cell
  pg_stat_user_tables HOT-ratio evidence). bench/compaction's driver
  gained -head-fillfactor plus head HOT/update stats for the
  compaction_head cells.

## 2026-08-22 — worker metadata history as append-only worker_log [0577]

- worker_log completes [0570]'s reservation: a full-snapshot row (name
  copied for join-free operator scans, metadata, target_instances,
  declared_by = ProcessIdentity, declared_at) appended in the same
  transaction as every worker create and metadata replace; machinery
  never reads it, no retention (worker_log/topic_log TTL revisit parked).
- registerWorker's replace path restructured onto replaceConfig's
  decide-before-writing shape: on insert conflict it reads the row with
  the comparison computed server-side (jsonb equality — Go never compares
  marshaled bytes against the normalized column) and returns without
  writing when the declaration matches. No-change redeclares stop writing
  entirely, ending the dead tuple per worker row per process start.
- Verified on a fresh DB: workerclaimlab's three consumers left exactly
  one log row per worker; a driven replace appended one row and a same-
  metadata redeclare appended none.

## 2026-08-22 — CLI --output json + json tags on public read-models [0575][0576]

- `--output <text|json>` root persistent flag: json stdout is exactly one
  parseable document per command, success or failure. Errors render on
  stderr as `{"error": {...}}` mirroring diagnostic.Error's LogValue parts
  (a plain failUsage/failOp error reduces to problem only); exit codes
  unchanged. failPrinted failures became result-document data
  (exists:false, exit 1 kept), so json stdout never carries prose.
- Every public read-model gained `json:"snake_case"` tags spelling the
  log-attr registry's keys (topic, version, group, message_id, *_count) --
  new CONVENTIONS.md rule under ## Package layout, the json sibling of the
  `db:` tag rule. JobRequest and Alert are stored payloads, so their
  stored key shape changed (pre-v1); the two lab mirrors reading old keys
  via SQL (cronlab, alertlab) swept.
- Durations render as unit-carrying strings, so composed or
  duration-carrying shapes got CLI-owned *Document structs beside their
  command; duration-free tagged read-models marshal directly.
- Mutations follow the surveyed conventions (kubectl/gh/aws/docker/stripe/
  gcloud): destroys emit small what-happened records and require --yes in
  json mode; cron run emits {cron_job, message_id}; rename echoes the
  get-shape; migrate emits summary documents; manager run rejects the
  flag; -q with --output json is a usage error.
- 42/42 fresh-DB labs (suite grew by compactiondeadlocklab, [0574]).

## 2026-08-22 — compaction-key deadlock evaluation [0574]

- Batched Produce proven cycle-free: the batcher's ascending-key sort is one
  global lock order, confirmed by the new compactiondeadlocklab
  (`just compaction-deadlock-lab`) and zero deadlocks across every
  bench/compaction cell.
- ProduceInTx confirmed as the one deadlock site; the 40P01 classifies
  transient and the caller's closure rerun lands both sides. Library-side
  retry rejected — guidance is to order ProduceInTx calls by compaction key.
- Hot-key serialization measured (bench/compaction/RESULTS.md): one hot key
  = ~50% of unkeyed throughput as a flat floor, not a cliff; the hurt case
  is hot key × many producer processes. ProduceOptions.Compaction and
  ProduceInTx doc comments updated; dead-tuple findings feed the
  fillfactor audit.

## 2026-08-22 — per-topic table split: cursor, lease, key_lease, compaction_head, binding, binding_log [0571]

- The six shared coordination tables became per-topic interpolated tables
  (cursor_<id>, lease_<id>, key_lease_<id>, compaction_head_<id>,
  binding_<id>, binding_log_<id>), applying the [0571] split rule: a topic's
  family grows 4 -> 10 tables and the shared schema reduces to exactly
  catalog + fleet + cross-scope history (system, topic, topic_log,
  consumer_group, worker, worker_instance, cron_job, migration_log).
- compaction_head_<id> dropped its topic_id column -- PK is compaction_key
  alone. Topic destroy became a DROP TABLE loop over all ten tables,
  deleting the three cross-table DELETEs (lease/key_lease via group-id
  subqueries, compaction_head by topic_id); the janitor's partition-drop
  and sweep cleanups lost their topic_id predicates the same way.
- Three verbs gained explicit topic context: ForceReclaimRange and
  DeclareBindings take topicId; KeyLeaseClaim/KeyLeaseData carry TopicId so
  Release can name the claim's table.
- The two cross-topic reads resolve topic ids from consumer_group and loop:
  ListBindingLog (one query per topic's binding_log_<id>) and the consumer
  group janitor's waiting-declaration sweep -- [0573]'s one batched DELETE
  per tick became one per topic's table per tick.
- 22 labs swept to the interpolated names; destroy assertions reshaped from
  0-row counts to table-absence (to_regclass). 41/41 fresh-DB suite.

## 2026-08-22 — binding_log retention via the consumer group janitor [0573]

- Waiting declaration rows older than a flat 7d TTL are swept in one
  batched DELETE per tick, keeping each declarer's newest waiting row so
  dead waiters stay visible in listings; installed rows are kept forever
  as the set-change audit. New OwnerSystem worker kind
  consumer_group_janitor under pkg/consumergroup/janitor -- hourly poll,
  Debug swept_count line only on ticks that deleted rows -- declared at
  RegisterSystem, provisioned by the system manager and every consumer's
  embedded manager.
- Naming pattern settled: each domain's cleanup worker is its janitor;
  the topic kind renamed "janitor" -> "topic_janitor".
- bindinglab extended with the sweep step (superseded rows deleted, each
  declarer's newest waiting row and all installed rows kept).

## 2026-08-22 — Topic config history as append-only topic_log [0570]

- The topic row stays the enforced truth (UNIQUE (name, schema_version),
  plain reads, rename = one UPDATE with 23505 -> ErrTopicNameTaken);
  topic_log records a full snapshot (name, partition_size, config,
  declared_by = common.ProcessIdentity, declared_at) in the SAME
  transaction as every create, config replace, and rename (one row per
  schema_version). Machinery never reads it — the binding [0511]
  current-table-plus-trail shape, now one pattern across both.
- Supersedes [0519], whose truth-in-declarations build was completed,
  lab-verified, then rolled back uncommitted: newest-row lateral joins
  leaked into every reader and (name, schema_version) uniqueness went
  procedural with advisory locks on every name write. A single
  append-only table with a stable topic id was evaluated and rejected —
  a repeating id cannot be a foreign-key target.
- `_log` confirmed as the append-only-history suffix:
  binding_declaration renamed binding_log (index binding_log_group; Go
  surface BindingLogData/BindingLogStatus/ListBindingLog; Declare*
  verbs and declared_by/declared_at stay); the parked failure-evidence
  table becomes worker_run_log, reserving worker_log for worker
  metadata history (ROADMAP Next).
- registeridempotencylab now asserts the trail (1 row on create, none
  on a no-change register, 2 after a config change);
  destroysystemlab's table list gains topic_log. 41/41 fresh-DB labs,
  `just verify` green.

## 2026-08-21 — Stop line as session summary [0567][0568][0569]

- The consumer stopped line is the session summary: bound identity,
  `duration`, and ten `<verb>_count` counters (zeros printed), emitted
  on every exit including fatal-error teardown, memory only. Declared
  VK0041 with a trailing `help` attr ("metrics explained: vulkan
  explain VK0041"); the VK0041 page is the counter catalog.
- Counters are metrics machinery on the instance-side MetricsProducer:
  atomics bumped in the message/exception runners from facts in hand,
  Snapshot() renders the line, and one Run tick loop
  (ProducerConfig.SessionFlushRate, 30s default) flushes changed totals
  as KindCounter series (session-uuid attr, one session per Consume
  call) and drains the abandoned-event queue as one batch per tick.
- vulkan.consumer.session.* are first-class declarations in the shared
  VK registry (diagnostic.Metric, VK0042-51): the flusher builds
  measurements from the declarations, otelvulkan renders Description as
  Prometheus # HELP, and `vulkan explain` resolves a metric by code,
  full name, or stop-line attr key — ten hand-written pages plus the
  index rows (including drifted VK0038-41).
- VK0052 "abandoned-routine events dropped" reports both failed batches
  and queue-cap drops (counted at enqueue, reported next tick); the
  registry completeness walk now links pkg/consumer and pkg/metrics.

## 2026-08-21 — Slow-operation threshold logging [0566]

- One Warn line when an operation runs past its duration threshold, at
  the [0559] boundaries: every produce entry point, the per-delivery
  dispatch, the worker tick (threshold = the row's own poll_rate, no
  config). ProducerConfig.SlowProduceThreshold and ConsumerConfig.
  SlowDispatchThreshold opt in (0 = disabled); three declared events
  VK0038-40 with docs pages; attr registry rows duration/threshold.

## 2026-08-20 — Record pipeline + repeated-line suppression [0564][0565]

- logging.NewPipelineLogger(sink, cfg) is now the one wrapper: its config
  declares the composition (Args, Buffer, Suppress), building over an
  existing pipeline merges instead of nesting, and internally each call
  is a record walked through a one-method handler chain in one fixed
  order (capture -> enrich -> suppress -> drain -> sink) [0565].
  LoggerWith/BufferLogger and their wrapper types deleted; ~70 call
  sites declare their composition; ring and WithLogBuffer untouched.
- Producer, consumer, and system manager instances declare Suppress at
  construction: repeats of one (level, message) Warn/Error line inside a
  one-minute window collapse to the first line plus suppressed_count on
  the next emission [0564]; suppressed_count joined the attr registry.

## 2026-08-20 — Coded declarations get their own package [0563]

- pkg/common/diagnostic now owns the error anatomy, the log events, and
  their shared VK registry (registry.go / error.go / event.go); LogEvent
  renamed Event (diagnostic.Event, NewEvent, Events(), KindEvent). Domains
  declare via diagnostic.NewError/NewEvent; common root is vocabulary
  only, beside subpackages diagnostic and logging.

## 2026-08-20 — Log events carry VK codes [0562]

- common.NewLogEvent registers operator-actionable Warn/Error log events in
  the errors' VK serial space; 12 declarations (VK0026-VK0037) in
  consumergroup/worker/cron logs.go + producer datastore, call sites log
  the declared Message with the code as the first attr, hand-written docs
  pages on /errors/, `vulkan explain` lists both kinds, conventions walks
  extended. Consumer start line finishes [0558]'s snapshot rule with
  message_timeout/shutdown_timeout/batch_limit.

## 2026-08-20 — Logging machinery carved out to pkg/common/logging [0561]

- New infrastructure subpackage pkg/common/logging owns the Logger seam:
  Logger, LoggerWith, NewDefaultLogger, BufferLogger, WithLogBuffer, the
  debug-buffer ring; toAttrs stays unexported, duplicated per side.
  Narrows [0528]: errors/retry stay flat (stdlib shadow;
  MessageOptions↔Error cross-import). ~315 qualifier renames across 130
  files; CONVENTIONS.md infrastructure kind now reads "common and its
  machinery subpackages".

## 2026-08-20 — Logging rule sheet + debug buffer + SQL owner comments [0558][0559][0560]

- Logging conventions written and swept [0558]: CONVENTIONS.md `## Logging`
  (levels by "who must act", static messages under the problem-line grammar,
  attr key registry, identity bound once, start line = diagnosis snapshot,
  silent steady state, log-or-return-never-both); all 108 call sites
  reclassified/rekeyed/reworded; default logger stdout -> stderr and the
  ~40 copied Logger field comments resolved in one sweep; labs count log
  events by level+attrs (createaheadlab off message text).
- Per-operation debug buffer [0559]: common.WithLogBuffer +
  common.BufferLogger hold Debug/Info/Warn in a bounded per-ctx ring and
  drain it into the first Error record's `preceding` group attr; boundaries
  at Produce/ProduceBatch, CallSafely, and every worker tick; tick failure
  Warn escalates to Error past the TickRetry curve's cap.
- SQL literal owner comments [0560]: 185 literals now open with
  `-- vulkan: <package>.<method>`, attributing pg_stat_statements and
  server-log lines to library verbs at zero runtime cost.
- `vulkan explain [code]` renders any declared error condition offline from
  the registry; migrate's advisory-lock release now threads ctx and passes
  error values (LogValue intact). 41/41 fresh-DB labs, `just verify` green.

## 2026-08-20 — Plain-error standard + package-kinds restructure [0554][0555]

- Plain errors standardized [0554]: CONVENTIONS.md "When writing a plain
  error" (templates/banned words/tense apply below the declaration
  boundary; constraint guards end `, got <value>`; names spelled as the
  caller knows them; errors.New for static text; fix clauses under the
  fix rules; wrap only the owned fact). Swept: 33 got-clauses, 6 static
  fmt.Errorf, off-template rewordings, the VK0004 raise moved from
  fmt.Errorf prose to .With. cron.ErrDeclarationInterrupted VK0025
  declared (+docs page) — the third deleted-mid-declaration race, missed
  by 0553's audit, now Transient-healed like VK0021/VK0024.
- Package kinds [0555]: every package is infrastructure, a domain
  (vocabulary root ← controller ← datastore + the workers maintaining
  its tables), or an API package (producer, consumer, admin,
  systemmanager — no declared errors, no SQL, no vocabulary). Seam law:
  what another stack imports is a vocabulary root or domain controller;
  own-tree-only imports nest freely. Placement law: a worker lives under
  the domain whose tables it maintains.
- Moves: NEW pkg/consumergroup (VK0014–16, Group, binding types,
  MessageMeta; ex-consumer/controller as ConsumerGroupController; base,
  the three subconsumers, cursoradvancer); worker/janitor →
  topic/janitor; worker/cronscheduler → cron/scheduler;
  worker/metricscollector → metrics/collector; admin's VK0008/VK0009 →
  pkg/topic. The binding-declaration datastore now raises
  consumergroup.ErrGroupNotFound — the gap that started the redesign.
- "door" banned from the vocabulary; live occurrences reworded.
  schema-gate-lab's stale pre-VK0023 text checks moved onto errors.Is.
- Verified: build/vet/`go test -race` across all three modules;
  41/41 fresh-DB labs.
- Standing plain-error walks [0556]: internal/errorregistry now
  ast-walks every plain raise string (banned words, static fmt.Errorf,
  missing got-clauses, declared-problem restatement) — the audits'
  wording half is a ratchet, not a manual pass.
- Developer tooling isolated [0557]: dev-only tools/ module (6th go.work
  entry); errorregistry became tools/conventions — one package named for
  the document it enforces; `just verify` is the blessed pre-commit/CI
  command (all five modules build, walks run); internal/ holds only
  live code.

## 2026-08-19 — Structured error anatomy shipped [0550][0551][0552]

- common.Error (pkg/common/error.go): code + recovery + problem + fix +
  values (slog attrs) + wrapped cause. NewError registers each code at init
  and panics on structural mistakes (malformed/duplicate code, unrecognized
  recovery, empty problem); With/Wrap return copies so declared Err*
  variables stay immutable; Error() renders
  `problem: name value -- fix [code]: cause`; errors.Is identity = code;
  LogValue() renders the parts as JSON-log fields; Docs() derives the page
  URL from one base-URL const.
- 19 codes assigned (VK0001–VK0019); every named error variable declares
  via common.NewError; ~30 raise sites moved onto .With value pairs;
  remaining plain validation errors swept onto the CONVENTIONS templates
  (enum Validates enumerate every legal value). A missing `__system.*`
  topic raises migrate.ErrNotRegistered everywhere [0552].
- Retry classification is consulted, never encoded [0551]: retry_error.go
  (marker types) and retry.go deleted; RetryDatastore is the one retry
  type; IsTransientDatastoreError (recovery first, then IsTransientPgError)
  is the one check; datastore errors surface unwrapped.
- CLI: renderErrorBlock is the single renderer for structured errors
  (aligned block: header + values/cause/retry-when-Transient/fix/docs);
  cliFixes rewrites a code's fix to a pasteable vulkan command (VK0017 →
  `vulkan migrate init`). `--output json` deferred to ROADMAP Later.
- internal/errorregistry: registry-wide tense + banned-word walks plus a
  source-scan completeness test that fails when a declaring package is
  missing from the import list. 19 hand-owned docs pages seeded under
  website/src/content/docs/errors/ (one per code, titled by the verbatim
  problem text; auto-generation rejected — convention + parked CI drift
  check keep them honest).
- Verified: 41/41 fresh-DB labs; all modules build; go test -race green.
- Same-day follow-on [0553]: the declaration boundary codified in
  CONVENTIONS ## Errors (cross-package brancher / recovery override /
  docs-worthy condition; validation and same-package signals stay plain),
  then a full audit of all ~618 raise sites: five promotions
  (VK0020 topic partitions remain; VK0021/VK0024 topic/worker
  declaration-interrupted races, Transient so DatastoreRetry heals them;
  VK0022/VK0023 schema version skew) and two topic-not-registered prose
  duplicates folded into ErrTopicNotFound. Five hand-written docs pages;
  affected labs green (register-idempotency, destroy-system,
  schema-evolution, consumergroup).

## 2026-08-19 — Definition/Provisioner split [0549]

- worker.Definition became a data struct (Name, Metadata, OwnerKind with
  "" = any kind, TargetInstances with 0 -> 1); the concrete machines are
  *XProvisioner, each building and storing its Definition at construction.
- Provisioner interface: Definition() replaces Name();
  Provision(ctx, declared *worker.Worker) replaces the id/owner/metadata
  triple -- the worker row is the declared form of the definition.
- One WorkerController.DeclareWorker(definition, owner) verb ends every
  Declare: 8 kinds' Declare collapsed to one-liners (consumers inherit it
  from BaseProvisioner, which stamps NoInstanceTarget as a base invariant);
  the alerts keep their group/binding preamble; the Declarer+Provisioner
  bundle interface is deleted. Declare returns only error -- provisioning
  re-reads the row so the newest declaration wins.
- 41/41 fresh-DB labs green; all five modules build, vet and -race clean.

## 2026-08-19 — Naming pass [0545][0546][0547][0548]

- Waterline retired [0545]: pkg/worker/waterline -> pkg/worker/cursoradvancer
  (worker name 'cursor_advancer'), AdvanceWaterline -> AdvanceCommitted,
  RollRetry -> AdvanceRetry; comments, labs, CLI help, and the justfile
  describe cursor.committed directly. reference/, bench/, and doc history
  keep the old word.
- Controller + datastore verbs dropped their own noun [0546] across Topic,
  CronJob, System, Compaction, KeyLease, CronScheduler, and
  ExceptionConsumer (now symmetric with DeliveryConsumer's bare Record*
  verbs); multi-noun controllers and the MessageAdmin facade keep theirs,
  each for a recorded reason.
- Run-side worker structs renamed *Instance [0547] (execution.go ->
  instance.go, manager pool -> instancePool/spawnedInstance);
  worker.Execution survives only as the interface name; the concrete
  Definition/Provisioner split was rejected -- a data-only definition has
  no consumer.
- Receiver letter codified as the type's final-word initial [0548]
  (CONVENTIONS.md amended); mechanical sweep: truncated names spelled out
  (sched, op, n, g, prev, idx, opts, msg; min/max builtin shadowing fixed),
  withMetadata moved into consumer_config.go x3, claimBuffer/rangeState/
  batchResponse/createAheadGate members unexported, cronscheduler's
  nine-column SELECT wrapped one per line.
- 41/41 fresh-DB labs green; root, cmd/vulkan, otelvulkan, examples, and
  bench modules all build; vet and -race clean.

## 2026-08-19 — Config & options refinement [0542][0543][0544]

- Shape decisions [0542]: Config keeps its name, backed by a new
  CONVENTIONS.md rule (Config = only optional fields; required values are
  constructor params — PostgresConnectionConfig's User/Host/Database moved
  into NewPostgresDatastore's signature); ProduceOptions compaction nested
  as Compaction *CompactionOptions{Key, Rank} built via
  NewCompactionOptions (nil = not compacted, rank-without-key
  unrepresentable); Consumer.Register keeps its five params and
  NewConsumerInstance unexported.
- Field grouping [0543]: config field order standardized domain-first with
  the ambient tail (Logger, Retry, per-loop retry curves) and codified;
  six drifted configs reordered (ConsumerConfig worst); cron_job.suspended
  and delivery's outcome-state/lease DDL columns regrouped; the two lease
  `RETURNING *` statements now name their columns.
- Dead-field pass [0544]: WorkerSnapshot.OldestInstanceAge chain and
  WorkerInstanceData.ExpiresAt deleted; JobRequest.CronJobId exempted as
  wire-payload contract; staticcheck + unparam clean across all three
  code modules.
- Verified by build/vet/race tests per change, targeted labs per chunk,
  and the full fresh-DB suite at close: 41/41.

## 2026-08-19 — examples/bench/reference split into dev-only nested modules [0541]

- Each tree got its own go.mod on the cmd/vulkan / otelvulkan pattern (no
  parent require; go.work resolves) but is never tagged or published — the
  release story stays three modules.
- Published module zip now carries the library only; root `go test ./...`
  dropped reference/waterline's tests. justfile lab recipes unchanged —
  `go run examples/phase_1/...` resolves through the workspace.
- The premise behind the roadmap's go.mod-cleanup follow-up was measured
  empty (root tidy is a no-op — pkg/ needs all three direct deps), so that
  item was dropped rather than carried.

## 2026-08-19 — Worker-tier surface review (Phase-13 rigor) [0540]

- Every surface the worker tier exports reviewed: pkg/worker vocabulary,
  pkg/worker/controller, the five worker kinds + manager Runner,
  pkg/systemmanager, the consumer split + consumer/base, pkg/producer, and
  the `vulkan manager` CLI. Verified by build/vet/race tests +
  metrics-lab + routing-lab.
- Shape fixes: Worker.Owner became *common.Owner (the lone by-value Owner);
  RegisterInstance's free-func-taking-the-controller became a
  WorkerController method (9 call sites); janitor's Provision validates
  owner before its pre-claim topic resolution (nil-owner panic);
  NewProducerInstance nil-checks cfg.
- The planted trap settled [0540]: bare sub-consumer constructors fenced
  by package/constructor docs (one worker row, not the assembled group;
  consumer.NewConsumer is the path), no signature change. Package comments
  sit below the package clause -- now a CONVENTIONS.md File layout rule.
- Text fixes: stale "first tick is uniform" comment deleted from all four
  kind configs; stale "pass" param name in InstanceTickRunner; in-code
  struct{}-vs-generics TODO deleted (ROADMAP owns it); stale MetadataValue
  mentions removed. No major readability debt surfaced beyond the
  convention sweep's fixes.

## 2026-08-19 — File-layout + blank-line conventions written and swept; LIFECYCLE demoted [0538][0539]

- LIFECYCLE left the public door: ConsumerType/CURSOR/LIFECYCLE,
  ConsumerConfig.Type and FanOutBatchLimit deleted, NewConsumer always
  builds the cursor path; deliveryconsumer is reachable only by direct
  import and carries an ON HOLD package doc. internal/ moves deferred.
- CONVENTIONS.md gained File layout [0538] (free vars/consts top, type
  block struct/New/validates, pair-by-pair or lifecycle order, unexported
  non-constructor free funcs at bottom behind the HELPERS banner) and
  Blank lines [0539] (bodies read as paragraphs: one blank between steps,
  glue rules, comments bind downward, switch/select arms stay dense).
- Rule-by-rule project-wide sweep (user-settled cadence): hygiene blanks,
  SQL-literal/exec glue (19), comment binding (45), validation preambles
  (17), paragraph steps (39), helpers moved behind banners in 29 files,
  constructor-before-methods fix; pair adjacency scanned clean. Verified
  by build/vet/race tests plus routing-lab; vendored cron and labs
  excluded by scope.

## 2026-08-18 — Layered-pattern chunk queue swept (pre-v1 cleanup) [0526]-[0537]

- The pkg-wide CONVENTIONS audit's chunk queue (expanded 2026-08-17 from
  ROADMAP) ran to empty over two days; 41/41 fresh-DB labs at close.
- Structure: migrate became a doored three-layer domain [0526][0527];
  logger/retry/errors/context merged into flat pkg/common [0528];
  compaction recorded as the deliberate two-layer exception (MessageRow is
  cross-stack vocabulary in common) [0530]; system-topic and cron-job
  declarations moved to their domains' controllers (topic_config.go)
  [0531]; consumer read-models live with the controller whose verbs return
  them [0532]; the metrics write door became pkg/metrics/producer
  (MetricsProducer, consumer/metrics deleted) [0534];
  consumer/base got pure constructors, BaseConsumerConfig /
  BaseDefinitionConfig, and symmetric ClaimKeyedRun/ReleaseKeyedRun
  [0535]; every worker kind carries a controller [0537].
- Rules settled: field absence is the zero value, never a nil pointer
  [0533], with MessageOptions the sanctioned nilable sparse sub-document
  [0536]; exported header-block fields (Config/Logger) are the standard;
  i* aliases for machinery-name collisions.
- Terminology sweeps: "park" family (~50 sites incl. parkStatement/parked
  CTE) and "ack" (AckMargin -> RecordMargin) replaced with the codebase's
  literal actions.

- datastore.Querier widened to the one statement contract
  (Exec/Query/QueryRow/SendBatch/CopyFrom — what pool, conn, and tx can all
  do minus transaction control); producer Tx = { Querier; Raw() pgx.Tx };
  the produce transaction is the one sanctioned package crossing, and
  cronscheduler produces through the producer's public InTransaction seam.
  pgx.Tx survives only in Begin-owning privates and the Tx adapter [0529].
- Every worker kind now carries a controller layer over its datastore:
  janitor, waterline, and cronscheduler grew controllers matching the alert
  kinds; executions call them; AdvanceWaterline's two-statement
  non-transactional advance reconfirmed and stated [0537].
- The Querier-interface ROADMAP item closed with this work.

## 2026-08-16 — Multi-message Produce [0525]

- ProducerInstance gained ProduceBatch(ctx, items...): every item in one
  transaction, none land unless all do, results in argument order, a
  failure named as "item N". ProduceItem{Message, Options} via
  NewProduceItem, which rejects a caller IdempotencyKey — one hot key
  would stall the batch's shared transaction, so keyed messages stay on
  Produce. No new write path: it drives controller.AppendMessageBatch (the
  batcher's flush verb); a private toAppend adapter fills options and
  generates the fresh v7 the datastore's ambiguous-commit rerun dedups on.
  Nothing dedups across calls, exactly as with unkeyed Produce.
- Dogfooded: collectConsumerGroup and both alert produceCheckSummary
  methods replaced their errgroup fan-outs with one ProduceBatch call each.
- producer-batch-lab's new produceBatchScenario proves the contract: 30
  items under a single xmin, ids ascending in argument order, a
  jsonb-poisoned item rolling the whole batch back with "item 2" in the
  error, caller-key and empty-batch rejections. Fresh-DB suite 36/36.

## 2026-08-16 — Alert pipeline instrumented [0524]

- The metrics collector's pass gained collectAlerts: fleet-level
  vulkan.alert.state.active_alerts / resolved_alerts gauges counted from
  the __system.alerts compaction heads, nil attributes, always produced.
  Per-name or per-severity series were rejected -- they would be enumerated
  from the heads themselves and go stale when heads sweep out of retention
  or a severity transitions.
- AlertController.Record returns RecordOutcome (active | resolved |
  nothing) beside its error, so a handler counts what its run did without
  a second head read.
- Both alert executions produce a per-run vulkan.alert.check.* summary --
  topics_evaluated / topics_failed / published_alerts / resolved_alerts,
  attribute alert=<name> -- at the end of every run INCLUDING failed ones,
  so a run that failed 1 of 9 topics no longer looks like a total failure.
  Head = latest run, retained log = one row per run; no cumulative counter
  state. The four produces run concurrently on one instance to share the
  producer's batched transactions; a failed summary produce joins the
  run's error.
- alertlab asserts the outcome of every classify arm and the summary after
  each executor run (including topics_failed = 1 on the corrupted-head
  run); metricscollectorlab's coverage set gained the state gauges.
  Fresh-DB suite 36/36.

## 2026-08-16 — Metrics collection and otel exposure [0522] [0523]

- pkg/metrics gained the measurement vocabulary: Measurement (name, kind,
  value, unit, attributes, at) — [0523] renamed the point type from Sample —
  plus the Metric* name consts under the reserved "vulkan." prefix,
  NewMeasurement and MeasurementKey. Measurements land on __system.metrics
  keyed by MeasurementKey(name, attributes), so compaction heads are the
  current value per series and the retained log is its history.
- pkg/worker/metricscollector: a system-scope worker on the cronscheduler
  template, declared at RegisterSystem and provisioned by SystemManager and
  every embedded consumer manager. Each pass at the row's poll_rate (default
  30s) produces fleet worker/cron measurements, then snapshots topics
  concurrently under TopicConcurrency with each group's measurements
  produced concurrently so the producer's batcher collapses them into shared
  transactions; __system.metrics itself is skipped by name.
- Reads: ListCompactionKeyMessages on CompactionController (the one new core
  verb), admin ListMeasurements / ListMeasurementMessages, and the CLI's
  `vulkan metrics list` / `vulkan metrics get`.
- otelvulkan nested module — core dropped its otel/prometheus deps outright
  (pkg/metrics/metrics deleted; release tagging is now a three-module
  story). Metrics registers an instrument per metric name on one meter
  (yours, or the global provider's by default); Exporter owns a private
  Metrics whose provider's only reader is the otel Prometheus reader and
  serves /metrics, registering names that appeared since the last scrape;
  MetricsProducer / MetricsConsumer publish and read user measurements on
  the same topic, with the reserved prefix rejected at Produce.
- `vulkan manager run --metrics-address` serves /metrics beside the manager,
  failing fast if the system isn't registered. metricscollectorlab drives
  collector -> topic -> admin reads -> a scraped manager subprocess under
  -race; fresh-DB suite 36/36.

## 2026-08-15 — Config becomes code-owned [0518] [0520] [0521]

- Config is declared in code and the latest declaration wins. RegisterTopic,
  RegisterCronJob and RegisterWorker (renamed from InsertWorker, since it
  creates-or-takes the declaration like every other register) each write their
  declared mutable config onto the row they find. All three report the same
  three outcomes at Info -- created, already existed, config replaced -- and
  the replaced line carries `field="old -> new"` for each field that actually
  moved, which is how two services declaring one thing differently gets found.
  Topic and cron do it in their datastore's `replace.go`; the worker's UPDATE
  returns both sides of its metadata through a self-join on the pre-SET row.
- A destroy racing a declaration is an error in all three paths, not a silent
  nil: the topic and cron registers used to return `(nil, nil)`, which their
  controllers dereferenced.
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
