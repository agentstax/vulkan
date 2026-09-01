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

### Chunk 15 -- seams, the schema, and close-out

Reshaped 2026-09-01. Two changes to the original: `vulkantest` left the
chunk (a doc page comes first -- ROADMAP), and schema-as-a-first-class
concern joined it. The second was not planned work: asking how
`vulkantest` would isolate one test from another surfaced that Vulkan
has no installation boundary at all, and the answer one-system-per-schema
needs is the same answer the tests needed. Eight tasks, each reviewed
before the next starts.

**Task 1 -- the instance interfaces.**

- `vulkan.Producer[Message]` is the instance's WHOLE public surface --
  `Produce`, `ProduceBatch`, `ProduceFunc`, `ProduceInTx`,
  `GetCompactionHeadInTx` -- not the three the spec named. A curated
  subset is a second definition of what a producer is, and anyone who
  batches writes their own anyway. `vulkan.Consumer[Message]` is
  `Consume`, which is already its whole surface.
  USER-SETTLED 2026-09-01, open to narrowing later.
- Compile-time assertions in `vulkan`; the client's Register verbs keep
  returning the concrete `*ProducerInstance[Message]`.
- Done when: build, `go test -race ./pkg/...`.
- Review: the interface is exactly the instance's public methods.

**Task 2 -- `internal/topic` becomes `pkg/topic/tables.go`.**

- The table-name funcs move to the vocabulary root and become public API
  (an operator writing a diagnostic query needs them, and labs cannot
  import `internal/`). Every `iTopic.` import rewrites. `internal/`
  holds nothing else and is deleted.
- CONVENTIONS ## Tables: the sentence "Library code names them ONLY
  through internal/topic's table-name funcs; labs, which cannot import
  internal/, interpolate the name inline" rewrites to name `pkg/topic`
  and drops the lab exception.
- Supersedes [0371], which is marked superseded; the record is [0628].
- Done when: build, `go test -race ./pkg/...`, targeted labs; the
  repo-root `internal/` is gone (`pkg/schedule/internal/robfig`, the
  vendored cron parser, is a package-level internal and stays).
- Review: a pure move -- no signature, no name, no behaviour changed.

**Task 3a -- "schema" gets one meaning.** [0629]

Found while planning task 3, not planned work. The word already carries
three senses: a payload's version (`SchemaVersion`, `schema_version`,
[0618] -- keeps its compound name, not in question), a migration target
(the CLI and `pkg/migrate`), and the Postgres namespace task 3 adds.
Postgres's own noun for the third is `schema`, and coining an alternative
is what ## Vocabulary bans, so the migration sense is the one that moves.

The fix is not a rename. The CLI has no collective noun that works
(`migrate up/down` already emits `{"scope": ...}` while `migrate status`
emits `{"schemas": [{"schema": ...}]}` for the same concept), and
`common.Owner` -- the library's real name for a polymorphic row's owner
-- reads backwards at a surface where nothing is being owned. Removing
the need for a collective noun closes all of it. USER-SETTLED
2026-09-01.

- `migrate status --output json` splits the list by what the rows are:

      {
        "initialized": true,
        "system_available": 3,
        "topic_available": 5,
        "system": { "current": 3, "available": 3, "behind": false },
        "topics": [
          { "topic": "orders", "current": 4, "available": 5, "behind": true },
          { "topic": "system", "current": 5, "available": 5, "behind": false }
        ]
      }

  The system is an object, not a list entry -- there is only ever one,
  the singleton [0625] settled. `topic` is already the log attribute
  registry's key.
- This fixes a real bug, not just a word. Only the `__system.` PREFIX is
  reserved (`pkg/topic/errors.go:87`), so a topic named exactly `system`
  registers, and today `migrate status` prints two rows both reading
  `system` -- in JSON and in text -- with nothing to tell the
  control-plane row from the topic. The split makes it unambiguous by
  construction.
- The result documents lose their collective noun the same way. The
  command already says what it targeted; the document says what
  happened:

      migrate system up --to 3    -> { "to": 3, "migrated_count": 1 }
      migrate topic up orders     -> { "topic": "orders", "to": 5, "migrated_count": 1 }
      migrate topics up --to 5    -> { "to": 5, "migrated_count": 12 }

  `scope`, `schema`, and `topic` restating each other all collapse.
- VK0017's problem line `"schema not registered"` becomes
  `"system not registered"` -- its raise site is the migrate read that
  finds no baseline row. Its docs page title is the verbatim problem
  text, so the page moves in the same change and codes.json re-syncs.
- The ~25 comments and help strings calling the shared tables "the
  control-plane schema" say "the control-plane tables". `cmd/vulkan`'s
  internal `type scope` stays -- it drives command construction and
  never reaches a user.
- ## Vocabulary gains the row: schema (a migration target) -> the
  resource itself, `system` or `topic`; `schema` is the Postgres
  namespace and nothing else.
- `--output json` is a contract [0575][0576]. Pre-v1, so the break is
  allowed, but the CLI golden-output tests move with it and the labs get
  grepped for `->>'schema'` before and after.
- DEFERRED 2026-09-01: "schema version" for a MIGRATION version stays as
  it is (VK0022/VK0023's problem lines, `ErrSchemaOlderThanBuild`/
  `ErrSchemaNewerThanBuild`, ~8 files of prose). The rule if it is ever
  taken up: `migration_log.migration_version` reads "migration version",
  `message_log.schema_version` keeps "schema version" -- so
  guides/schema-versions.mdx, which is about payloads, never moves.
- Done when: build, `go test -race ./...`, `just verify`, the CLI golden
  tests, `npm run verify` for the moved error page.
- Review: no surface says "schema" about anything but a Postgres schema;
  a topic named `system` reads unambiguously in both output modes.

**Task 3 -- the schema is the installation.** [0630]

The design: a schema is one Vulkan installation. That is what makes
`system_config`'s singleton row and `System()`'s nameless handle correct
permanently -- multiple systems come from multiple schemas, not from a
`system_id` dimension growing through every read. It also stops Vulkan
scattering its shared tables and its ten-per-topic tables through the
user's `public` schema, which is why river, pgmq, and graphile-worker
all default to a schema of their own.

- `PostgresConnectionConfig.Schema`, default `"vulkan"`.
  `NewPostgresDatastore` sets `poolConfig.ConnConfig.RuntimeParams
  ["search_path"]` to `"<schema>, public"` and `PostgresDatastore`
  carries the resolved name (task 4 needs it).
- REJECTED: schema-qualifying the SQL. 314 shared-table references across
  62 files and 207 SQL literals, every one of which would become a
  Sprintf -- that breaks the ## SQL rule that a literal is a constant raw
  string. `search_path` costs one line and changes no SQL.
- `public` stays second in the path because `InTransaction` hands the
  user a `Tx` on Vulkan's pool: their `INSERT INTO orders` has to keep
  finding `public.orders`. Documented caveat: a `CREATE TABLE` a user
  runs inside `InTransaction` lands in Vulkan's schema.
- The name is interpolated into `CREATE SCHEMA` and into the search_path
  runtime param, so `Validate` guards it against an identifier pattern.
  Quoting is not the answer -- a rejected name is.
- `RegisterSystem` runs `CREATE SCHEMA IF NOT EXISTS` as its first
  statement inside the lock it already takes. Without it the search_path
  falls through and every `CREATE TABLE` silently lands in `public`.
  Missing CREATE privilege (42501) is a new failure mode: a declared
  error, next VK code, Permanent, its fix naming the GRANT, its
  hand-written page landing here.
- `DestroySystem` leaves the schema standing (USER-SETTLED 2026-09-01).
  It drops the tables it created; the namespace may be shared or
  pre-created by a DBA, and dropping it is past what the verb promises.
- Hazard to state on the page, not engineer around: with the schema
  absent, reads fall through to `public` and an older install's tables
  are still there to be found. Pre-v1, and a dev database is recreated.
- Decision record [0630] is written in this task -- the mechanism settles
  here, not at close-out.
- Done when: build, tests, drop+recreate of the dev DB, a lab proving two
  clients on two schemas register the same topic name independently.
- Review: no new column, no schema value stored in any row. The schema is
  the boundary exactly as a per-topic table's name is its own.

**Task 4 -- the six advisory lock keys take the schema.**

None of these is a correctness fix. `search_path` already isolates the
tables, so two installations are writing to different tables either way.
Every one is a contention fix: two installations serializing on a lock
neither needs.

- `common.AdvisoryLock` -- one int constant shared by system register,
  system delete, and migrate -- becomes a per-schema derived key.
- `hashtext('topic:' || $1)`, `hashtext('schedule:' || $1)`, and
  `hashtext(format('consumer_group:%s:%s', ...))` gain the schema.
- `advisoryLockKey(topicId, partition)` packs two int32s into an int64
  and has no bits to spare, so it becomes
  `hashtext(format('partition:%s:%s:%s', schema, topic_id, partition))`
  -- one uniform mechanism across all six. It is not a hot path: it runs
  from the create-ahead gate at 80% and 95% of a partition's id range and
  from the missing-partition self-heal, never per produce. The packed
  int's exact no-collision guarantee is given up; a hashtext collision
  only makes two unrelated partition creations wait, which the three
  string-keyed locks already accept.
- `migrate.IsLocked` reads `pg_locks WHERE objid = $1` and follows the
  new key with no change.
- Done when: build, tests, the two-schema lab from task 3 extended to
  prove the two registers do not serialize.
- Review: all six sites are one shape; nothing derives a key twice.

**Task 5 -- the schema reaches the operator.**

- 18 diagnose queries across `pkg/{topic,consumergroup,migrate,common}/
  errors.go` name a table. Pasted into psql by an operator whose own
  search_path is `public`, `to_regclass('message_log_{topic_id}')`
  returns NULL.
- SETTLED 2026-09-01: the queries take a `{schema}` placeholder AND
  `schema` is attached at every raise site whose declaration carries one
  of them. Diagnose queries are exempt from the attachable-at-every-site
  rule, so an unattached `{schema}` would silently drop the query from
  the operator's output -- attaching it is what keeps all 18 usable. The
  datastores already hold the resolved name; the controller and admin
  raise sites need it plumbed. `schema` joins the CONVENTIONS attribute
  registry in this task.
- CLI: a `--schema` flag and its env var, threaded to the connection
  config; `vulkan explain` unaffected.
- Labs interpolate table names inline -- they run on the client's pool so
  search_path covers them, but any lab opening its own connection needs
  the schema.
- `just compat-lab` breaks silently: the pinned prior build has no
  `Schema` field and writes to `public` while the working tree writes to
  `vulkan`, so the two never meet. The lab must pin the new build to
  `public` for the comparison to mean anything.
- Done when: `just verify`, the affected labs, `just compat-lab` green
  against the pinned tag.
- Review: an operator pastes a diagnose query and it runs.

**Task 6 -- the docs lose PROPOSED.**

- guides/client.mdx and guides/consumer-group-config.mdx drop the
  `ThreadAside label="PROPOSED"` and the eleven `// PROPOSED -- not
  shipped API.` sample headers; both descriptions stop saying "proposed".
- Stale text goes with them: client.mdx's Names section still promises a
  typed instance exposes `Registered` and `Declaration`, and neither
  shipped (chunk 12 reverted in full). The Open section's `RequireMatch`
  and `Data`-sweep bullets are answered or done.
- Every site sample re-checked against the shipped library; playground
  headers re-scored (concepts held, traps hit).
- A schema section lands here -- the default, the `search_path` value and
  why `public` is second, the `InTransaction` caveat, and the privilege
  requirement.
- Done when: `npm run verify` in website/, vale clean.
- Review: nothing on the site describes API that does not exist.

**Task 7 -- ROADMAP gains what left.**

- `vulkantest`: a doc page comes first, then the build. Carry the sizing
  (roughly 50 lines for `NewClient(t)` on an ephemeral database, 30 for
  running a consumer inside a test) and the finding that per-schema
  isolation was rejected for it -- ephemeral database, since advisory
  locks are database-scoped.
- Anything task 5's open question defers.

**Task 8 -- close-out.**

- HISTORY.md entry citing [0625] and [0628]-[0630]; this section removed; the
  ROADMAP item removed; the memory marked shipped.
- Done when: full fresh-DB suite, `just verify`, `npm run verify`,
  `just compat-lab`.
