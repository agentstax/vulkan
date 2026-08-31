# TODO

Sliding window of in-flight work only. Future work lives in ROADMAP.md;
shipped work in HISTORY.md; decision rationale in DECISIONS.md ->
docs/decisions/.

## One-client API shape [0625]

Spec: website guides/client.mdx (the shape) and
guides/consumer-group-config.mdx (the stored group config). One chunk at
a time; each ends green on its own checks and is reviewed by hand before
the next starts. Every chunk lists what it touches, when it is done, and
what the review should look at. Nothing later in the list is started
early -- a chunk that turns out too big is split, not stretched.

Order of intent: renames first (mechanical, wide, no behaviour), then
the client and its handles beside the existing packages (additive, the
old API keeps compiling), then the cut-over (old packages go internal,
everything moves), then the behaviour that needed the client to exist
(stored group config, outcomes, the liveness warn), then seams and
close-out.

### Chunk 1 -- `*Data` -> `<Table>Row` in the datastore layers

- Every table-exact scan struct under `*/controller/datastore/model.go`
  (33 today) and any `*Data` scan struct living beside a query renames to
  the table it scans plus `Row`: `TopicConfigRow`, `WorkerConfigRow`,
  `ScheduleConfigRow`, `MessageLogRow`. The `db:` tags do not move.
- CONVENTIONS ## Package layout: the `*Data` sentence flips to `*Row`.
  tools/conventions gains a walk refusing `*Data` under a datastore path.
- Done when: `go build ./...`, `go test -race` on every datastore package,
  `just verify` green; `rg "type [A-Za-z]+Data struct" pkg` returns only
  non-datastore hits.
- Review: names say the table; nothing else changed.

### Chunk 2 -- read-models take `<Noun>Data`

- `topic.Topic` -> `TopicData`, `schedule.Schedule` -> `ScheduleData`,
  `system.System` -> `SystemData`, `worker.Worker` -> `WorkerData`,
  `consumergroup.Group` -> `GroupData`, `producer.MessageRow[T]` ->
  `MessageData[T]`. Still in their vocabulary packages. Reports and
  status types (`GroupStatus`, `MessageStatus`, `TopicSnapshot`,
  `Declaration`) keep their names.
- json tags untouched; `--output json` unchanged (grep labs for
  `->>'[A-Z]` anyway).
- Done when: build, tests, CLI golden output diff empty, targeted labs.
- Review: the sweep is total -- no read-model left without the suffix.

### Chunk 3 -- the `vulkan` package and `NewClient`

- Root package `vulkan`: `NewClient(ds, &ClientConfig{AllowDestroy,
  Logger, Retry})` builds and holds today's five assemblers. Type
  aliases for every user-spelled type (`ConsumerConfig`,
  `MessageOptions`, `RetryPolicy`, `TopicConfig`, `DestroyOptions`, `Tx`,
  `MetaFromContext`, `Terminal`, `Delay`, `LifecycleContext`, ...).
- Client verbs that delegate with today's argument shapes:
  `RegisterConsumer[T]`, `RegisterProducer[T]`, `RegisterSchedule[T]`,
  `RegisterTopic`, `RegisterSystem`, `ListTopics`, `ListSchedules`,
  `ListDeclarations`, `MigrateTopics`, `ListAlerts`, `ListMeasurements`,
  `ListMeasurementMessages`, `RunManager` (the one SystemManager, its
  permit).
- Old packages untouched and still public. Playground 01 and 03 rewritten
  to the client as the demo; nothing else moves yet.
- Done when: build, `go test -race ./...`, playground 01 + 03 run.
- Review: `client.` autocomplete is the whole verb list; no logic in the
  root package beyond construction and delegation.

### Chunk 4 -- `Topic` and `Group` handles

- `client.Topic(name)` -> `*vulkan.Topic` (no I/O): `Get` (comma-ok),
  `Migrate`, `Rename`, `Destroy`, `Health`, `Metrics`, `CompactionHead`,
  `ListKeyMessages`, `ListGroups`, `Group(name)`. `*vulkan.Group`: `Get`,
  `ListWorkers`, `Destroy`.
- New reads: list groups by topic and get one group's row, on the
  consumer group controller/datastore; `CompactionHead` and
  `ListKeyMessages` route to the compaction controller by the topic's id.
- `MessageAdmin`'s flat methods stay until chunk 7.
- Done when: build, tests, a lab per new read, `vulkan topic`/`group`
  CLI verbs still pass.
- Review: every handle verb resolves the name at call time; `Get` is the
  only `(nil, nil)`.

### Chunk 5 -- `Schedule` and `System` handles

- `client.Schedule(name)` -> `*vulkan.Schedule`: `Get`, `Suspend`,
  `Unsuspend`, `Run`, `Status`, `ListMessages`, `Destroy`, and
  `Schedule(ctx)` running the client's one SystemManager (a second
  concurrent run in one process is refused by the permit -- today it
  builds a rival manager). `RegisterSchedule` returns this handle.
- `client.System()` -> `*vulkan.System`: `Get`, `Migrate`, `Destroy`.
- Done when: build, tests, schedulelab, `vulkan schedule`/`system` CLI
  verbs pass; a lab proving two `Schedule` calls in one process refuse
  the second.
- Review: one handle type carries both the admin verbs and the run verb.

### Chunk 6 -- ambient once

- `Logger`, `Retry` leave every user config (`ConsumerConfig`,
  `ProducerConfig`, `ScheduleConfig`, `TopicConfig`); `AllowDestroy`
  leaves `MessageAdminConfig`. `MessageAdminConfig`,
  `SystemManagerConfig`, `SchedulerConfig` dissolve into `ClientConfig`.
- Done when: build, tests, targeted labs; a `ConsumerConfig` literal with
  `Retry` no longer compiles.
- Review: the `Retry` vs `Message.Retry` trap is gone by construction.

### Chunk 7 -- the cut-over

- pkg/consumer, pkg/producer, pkg/scheduler, pkg/admin,
  pkg/systemmanager move under internal/ (the CLI keeps access by the
  import-path prefix rule). `producer.InTransaction`/`Tx` become
  `vulkan.InTransaction`/`vulkan.Tx`. `MessageAdmin`'s flat methods are
  deleted; the client and handles are the only path.
- Every declared fix or help text naming a moved surface
  (`MessageAdmin.RegisterTopic` in VK0005, `MessageAdmin.RegisterSystem`
  in VK0017, ...) rewords to the client verb, its docs page updated
  verbatim in the same change. The `-- vulkan: <package>.<method>` SQL
  owner comments follow the moved packages; the website sandbox SQL
  mirror and codes.json re-sync.
- Every lab, every playground, the CLI, and every site sample compiles
  against `vulkan` only.
- Done when: build, `go test -race ./...`, the full fresh-DB lab suite,
  `just verify`, `npm run verify` in website/.
- Review: a user-facing file imports `datastore` and `vulkan` and nothing
  else from the module.

### Chunk 8 -- `ScheduleSpec` and nil-able options

- `RegisterSchedule[T](ctx, ScheduleSpec{Name, Topic, Cron}, payload,
  cfg)`. `ProduceOptions`, `DestroyOptions`, `Schedule.Run`'s config
  become `*T` accepting `nil`; every `ProduceOptions{}` call site drops
  to `nil`.
- Done when: build, tests, producer/schedule labs.
- Review: one rule for every optional parameter; no value-typed options
  remain on a verb.

### Chunk 9 -- group config split, types only

- `ConsumerConfig` keeps the group fields (`Message`, `MessageMin`/`Max`,
  `ConcurrencyOverride`, `Start`, `Bindings`, `ExceptionInitialBackoff`,
  `MaxRangeReclaims`) at `RegisterConsumer`; the bindings argument folds
  into `Bindings`. `ConsumeOptions` (nil-able) at `Consume` holds the
  session fields (`BatchLimit`, `QueueSize`, `MessageConcurrency`, poll
  rate, margins, `TimeoutGrace`, `SlowDispatchThreshold`, `InstanceTTL`,
  `BindingRetryInterval`, `ShutdownTimeout`, `DisableGracefulShutdown`).
- Nothing stored yet: the instance still runs on its own copy.
- Done when: build, tests, consumer labs, `ShutdownTimeout` default still
  derives from `MessageMax.Timeout` + grace + margin.
- Review: the two-replicas-differ classifier put every field on the right
  side.

### Chunk 10 -- the declaration is stored and read back

- `RegisterConsumer` writes the group document into the group-owned
  `worker_config` rows (the metadata replace path; `worker_config_log`
  snapshots it with `declared_by`/`declared_at`); a differing overwrite
  logs a declared Warn naming the diff. Sparse document, defaults resolved
  at read. `Consume` reads the stored document at start instead of using
  the process's copy.
- The differing-overwrite Warn is a declared event: next VK code, and
  its hand-written docs page lands in this chunk.
- No refresh loop yet.
- Done when: build, tests, a lab where two processes declare different
  documents and the second's Warn fires and wins.
- Review: no new table, no new column; the write is the existing replace.

### Chunk 11 -- live refresh

- A background loop per instance refreshes a mutex-guarded copy on a
  poll cadence; `ConcurrencyOverride` pins per claim; `ShutdownTimeout`
  re-derives when `MessageMax.Timeout` moves. Claiming never reads the
  row.
- Done when: a lab redeclares `MaxRetries` under a running group and a
  message at attempt 3 gets attempts 4 and 5.
- Review: the hot path has no config read; the pin closes the
  exclusive-and-parallel-at-once window.

### Chunk 12 -- `Declaration` outcome and `RequireMatch`

- `Declaration` (`DeclarationCreated` / `Joined` / `Updated`) on the
  consumer and schedule instances. `ConsumerConfig.RequireMatch` returns
  `ErrGroupConfigMismatch` (next VK code, Permanent, docs page) on a
  differing stored document and writes nothing.
- Done when: build, tests, a lab per outcome, the error page lands.
- Review: the outcome is a value, not a log line.

### Chunk 13 -- a stale build cannot declare

- A build whose document would drop fields a newer build declared is
  refused with a declared error, on the migration gate's
  `min_compatible_version` mechanics; consuming is untouched.
- Done when: `just compat-lab` against the prior tag shows the verdict.
- Review: the gate refuses the write only.

### Chunk 14 -- the producer's liveness warn

- A fourth evaluator in `RegisterProducer`'s register-time alert pass:
  no `worker_instance` row for this topic's `topic_janitor` with
  `expires_at > now()` -> declared Warn event (next VK code, docs page)
  naming the topic and `vulkan manager run`.
- The client's start line gains `runs_manager` -- whether this process
  runs upkeep (`RunManager` or a handle's `Schedule`).
- Done when: a lab with a produce-only process logs it once and a
  consumer's start silences the next restart.
- Review: log-only; `Register` never fails on it.

### Chunk 15 -- seams and close-out

- `vulkan.Producer[T]` (`Produce`, `ProduceFunc`, `ProduceInTx`) and
  `vulkan.Consumer[T]` (`Consume`) interfaces satisfied by the instances;
  `vulkantest.NewClient(t)` (own module under tools/ or a nested module)
  runs the real library on an ephemeral schema.
- guides/client.mdx and guides/consumer-group-config.mdx lose their
  PROPOSED asides; every site sample re-checked against the shipped
  library; playground headers re-scored (concepts held, traps hit).
- HISTORY.md entry citing [0625]; this section and the ROADMAP item
  removed; memory of the proposal marked shipped.
- Done when: full fresh-DB suite, `just verify`, `npm run verify`,
  `just compat-lab`.
