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

- The assembler packages stay where they are for now (USER-SETTLED
  2026-08-31: no internal/ move, `MessageAdmin` keeps its methods -- the
  client delegates to them). `producer.InTransaction`/`Tx` become
  `vulkan.InTransaction`/`vulkan.Tx`.
- Every declared fix or help text naming an assembler surface
  (`MessageAdmin.RegisterTopic` in VK0005, `MessageAdmin.RegisterSystem`
  in VK0017, ...) rewords to the client verb, its docs page updated
  verbatim in the same change; codes.json re-syncs.
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

### Chunk 12 -- `RequireMatch`

MANUAL DENIED BY USER SHIT CODE AND SHIT IMPLEMENTATION
WHO IS GOING TO USE THIS???? A user has to set RequireMatch
and then they can never change it without setting it to false
then deploying then updating then setting it back to true???
who in their right god damn mind would want that. Horrible suggestion

- `ConsumerConfig.RequireMatch` returns `worker.ErrWorkerConfigMismatch`
  (VK0061, Permanent, docs page) on a differing stored document and
  writes nothing. Raised by the worker datastore inside the same
  transaction as the compare, before the UPDATE, so no window exists.
- The `Declaration` outcome the chunk originally carried is cut to
  ROADMAP Later -- it reported, it did not protect; VK0059 already
  reports an overwrite. Its client.mdx sections keep their PROPOSED
  marks in chunk 15 rather than being unmarked or deleted.
- Also in this chunk: every typed instance exposes the row it resolved
  as `Registered` (client.mdx:404), and the worker datastore takes a
  `WorkerDeclaration` write shape like the schedule datastore's.
- Done when: build, tests, a lab proving VK0061 leaves row and log
  untouched, the error page lands.
- Review: the refusal happens before the write, not after.

### Chunk 13 -- a stale build cannot declare

CUT 2026-08-31, nothing built: same lock-with-no-key as chunk 12 (a
rollback cannot start), and additive on the migration gate's own terms.
Parked in ROADMAP's parking lot under "Strict declaration forms" with
the full reasoning; the spec section was deleted from
guides/consumer-group-config.mdx.

### Chunk 14 -- the worker liveness alert -- BUILT [0627]

Reshaped 2026-08-31 from "the producer's liveness warn": the janitor case
is one row of a real alert, not a bespoke check. Built 2026-09-01 as the
third built-in alert; what shipped is [0627], and the plan's threshold
did not survive the build (see below).

- `pkg/alert/workerliveness`, file for file the `partitioncount` pattern:
  provisioner, `Declare`, instance (`Run` -> consume the schedule's
  message -> `evaluateTopics` -> `produceCheckSummary`), `job.go`
  (`alert.worker_liveness`, exclusive, `@hourly`), `JobConfig`, metadata,
  configs, `controller/` with `Evaluate` and the alert adapter.
- The fact is `metrics.WorkerSnapshots`, the read `admin` already
  composes, filtered to the owner topic: its `topic_janitor` plus every
  row its consumer groups declare. The controller owns no SQL.
- The condition is the existing classification `metrics.WorkerUnclaimed`
  (`target_instances != 0`, no live instance row). NO threshold: the
  manager DELETEs expired instance rows every tick, so `UnclaimedFor` is
  0 for exactly the workers the alert is for, and
  `live < target` never trips for the `NoInstanceTarget` (-1) group
  consumers. The cron cadence plus the alert's repeat/resolve is the
  debounce.
- Wiring: `RegisterSystemConfig.WorkerLiveness`, the third job in
  `admin/system.go`, the provisioner in `admin.alertDeclarers` and the
  system manager's list, the controller in the producer's evaluators.
  The consumer's pass is untouched -- its own `Consume` starts the
  workers the check would miss.
- Accepted: a produce-and-consume process warns once on a cold start
  (`Register` runs before `Consume` claims anything; rows linger the 30s
  `InstanceTTL`); dev `go run` after a pause; a full-outage restart.
  Silent under rolling deploys and restarts inside the TTL.
- The register-time line `alert condition holds` is declared VK0063 for
  all three built-ins, one hand-written page. The alert's own message
  clause moved from the `message` attribute to `alert_message` at every
  site, `AlertController`'s two lines included; both keys are in the
  CONVENTIONS registry.
- `runs_manager` on a client start line: cut. The client has no start
  line and cannot know at construction; `manager instance starting`
  already prints in every process that runs upkeep.
- Lab `workerlivenesslab` (`just worker-liveness-lab`): a produce-only
  `RegisterProducer` warns VK0063 naming the unclaimed `topic_janitor`;
  under a live consumer the next `Register` is silent; with the consumer
  stopped a job run publishes an active alert whose evidence names the
  group's `message_consumer`, and restarting the consumer resolves it.
- Docs: guides/client.mdx's manager section rewritten to the alert,
  `runs_manager` gone; errors/VK0063.md; codes.json re-synced.
- Green: build, `go test -race ./pkg/...`, `just verify`,
  `just worker-liveness-lab` (twice, and it leaves no alert heads
  behind), `just alert-lab`, the site's vale/remark/prettier/vitest.

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
