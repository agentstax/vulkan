# TODO

Sliding window of in-flight work only. Future work lives in ROADMAP.md;
shipped work in HISTORY.md; decision rationale in DECISIONS.md ->
docs/decisions/.

## One declaration per type, vulkan as the client plus aliases

Picked up 2026-09-03 from ROADMAP Now ("`vulkan` as the real home of every
user-spelled type"). The design settled the OPPOSITE of that item's
direction: declarations stay where their lowest reader is, and `vulkan`
aliases them. Decision record to write when this plan is accepted (next
number after 0642); it amends [0625]'s "every user type in vulkan" clause
and [0555]'s package-kinds text.

### The laws

1. **One declaration.** Every exported type, const, named error, and
   declared event is declared once. The only `type X = pkg.X` lines in the
   repo are in pkg/vulkan/alias.go.
2. **Placement by lowest reader, with a floor.** A declaration lives in the
   lowest package that reads it. Machinery (controller, datastore, workers,
   batcher) declares nothing a user spells except its own Config and `*Row`
   structs. So every user-spelled name lives in exactly one of: `common`
   (two domains read it), a domain root (that domain's machinery reads it),
   or an assembler (only its own verbs read it). Codes are the same law
   with no assembler case: every `diagnostic.NewError` / `NewEvent` /
   `NewMetric` lives in a domain root's `errors.go`, `logs.go`, or
   `metrics.go`, exported, or in `common` when two stacks raise it.
   Machinery and assemblers declare none.
3. **Roots are named for what they are about.** Thing domains take the
   resource: topic, system, worker, alert, metrics. Activity domains take
   the verb: schedule, migrate, compaction, consume, produce. An activity
   domain's assembler is its agent noun: scheduler, consumer, producer. A
   root's own controller and datastore are `<Root>Controller` /
   `<Root>Datastore` (ScheduleController is the template). Type names keep
   their prefix for now (`produce.ProduceOptions`, `consume.GroupData`);
   dropping it is a separate pass.
4. **vulkan is the client plus aliases.** It declares Client, ClientConfig,
   NewPostgresPool and its config, the four handles, the two instance
   wrappers, and the Producer/Consumer interfaces. Every other exported
   name is an alias or var into the declaring package. No twin structs, no
   adapters. The client holds assemblers only (admin, scheduler,
   systemmanager) and a handle verb is one call.

### Tree after

```
pkg/common                         + Versioned SchemaVersionOf            <- topic  (review G1)
                                   MessageOptions MessageData ConcurrencyPolicy RetryPolicy Owner LifecycleContext errors
pkg/datastore                      + Tx TransactionFunc InTransaction     <- produce/controller/datastore

pkg/topic                          TopicData DeliveryLogMode errors events
                                   + TopicConfig (WithDefaults, Validate, ToTopic)   <- topic/controller
pkg/topic/controller, /controller/datastore, /janitor, /migrations

pkg/produce                        NEW root: ProduceOptions CompactionOptions NewCompactionOptions ProducerFunc
pkg/produce/controller             <- producer/controller      ProduceController (rename)
pkg/produce/controller/datastore   <- producer/controller/datastore   ProduceDatastore (rename)
pkg/produce/batcher                <- producer/batcher
pkg/producer                       assembler: Producer NewProducer Register ProducerInstance ProducerConfig
                                   ProduceItem NewProduceItem ProduceResult
                                   DELETED: options.go transaction.go message_data.go (re-alias chain)

pkg/consume                        <- consumergroup (git mv): GroupData BindingDeclaration MessageMeta CursorPosition
                                   MetaFromContext WithMeta Terminal Delay Beginning Head errors events
pkg/consume/controller             ConsumeController / ConsumeDatastore (rename)
pkg/consume/messageconsumer, exceptionconsumer, deliveryconsumer, cursoradvancer, janitor, base   (paths only)
pkg/consumer                       assembler: Consumer NewConsumer Register ConsumerInstance ConsumerConfig
                                   ConsumeOptions ConsumerFunc

pkg/schedule                       ScheduleData GroupStatus MessageStatus StoredMessage Expression errors events
                                   + ScheduleSpec{Name, Topic, Cron}                <- vulkan
                                   + ScheduleConfig (WithDefaults, Validate)        <- schedule/controller
pkg/schedule/controller            Register(ctx, systemId, spec *schedule.ScheduleSpec, topicId, payload, cfg)
                                   -- parses spec.Cron itself (validation lives at the controller)
pkg/schedule/producer
pkg/scheduler                      Register[M](ctx, spec *schedule.ScheduleSpec, payload *M, cfg *schedule.ScheduleConfig)

pkg/admin                          keeps DestroyOptions RunScheduleConfig RegisterSystemConfig VersionHealth
                                   + GetGroup(ctx, topic, group) (*consume.GroupData, error)   (nil, nil) when topic or group absent
                                   + ListGroups(ctx, topic) ([]*consume.GroupData, error)
                                   + ListGroupWorkers(ctx, topic, group)          today's worker-returning GetGroup, renamed
                                   + GetCompactionHead[M](ctx, topic, key) (*common.MessageData[M], error)
                                   + ListKeyMessages[M](ctx, topic, key, limit)
                                   RegisterTopic takes *topic.TopicConfig

pkg/system, /controller, /migrations; pkg/systemmanager; pkg/worker, /controller, /manager;
pkg/metrics, /controller, /collector, /producer; pkg/alert/...; pkg/compaction/controller; pkg/migrate   unchanged

pkg/vulkan
  client.go  client_config.go  pool.go  postgres_connection_config.go
  topic.go  group.go  schedule.go  system.go
  producer.go  consumer.go                    the two interfaces
  producer_instance.go  consumer_instance.go
  alias.go  errors.go  events.go
  DELETED: adapter.go schedule_spec.go consumer_config.go producer_config.go batcher_config.go
```

### vulkan after

- alias.go: every right-hand side is the declaring package -- common,
  datastore, a root, or an assembler. Never a `/controller` or
  `/datastore` path. The full alias set is whatever the closure test
  (review G3) reports, not a hand-kept list.
- errors.go: `var ErrTopicNotFound = topic.ErrTopicNotFound` for every
  exported `Err*` in topic, consume, schedule, system, worker, common,
  migrate (31 today). Same pointer, so `errors.Is` holds under either
  name.
- events.go: every exported `Event*` the same way (20 today, in topic,
  consume, schedule, worker, metrics, alert).
- client.go: Client{Config, Logger, ds, admin, scheduler, manager}. The
  group and compaction controllers are gone. RegisterProducer and
  RegisterConsumer fill `cfg.Logger` and `cfg.Retry` from the client when
  the caller left them nil -- two `if` statements, no helper -- then call
  the assembler's constructor with the caller's own config type.
- RegisterSchedule takes `*ScheduleSpec` and passes it down unchanged.
- producer_instance.go: `ProducerInstance[M]{instance *producer.ProducerInstance[M]}`,
  six one-line pass-through methods. consumer_instance.go keeps its
  errgroup body and holds the instance as a private field, not embedded.
- topic.go / group.go: every verb one admin call. Topic.CompactionHead,
  Topic.ListKeyMessages, Topic.ListGroups, Group.Get, Group.ListWorkers
  stop composing in vulkan.

### Lower-package deltas

- `consumer.ConsumerConfig` gains `Bindings []string` (nil = the whole
  topic) and `Consumer.Register` drops its `bindings` param (review G2).
- `topic.TopicConfig`, `schedule.ScheduleConfig` move with their
  WithDefaults/Validate; controllers import the root for them.
- `common.Versioned` + `common.SchemaVersionOf` (review G1).
- `datastore.Tx` (Querier + `Raw() pgx.Tx`), `TransactionFunc`,
  `InTransaction`, and the private `vulkanTx`/`newTx` move to
  pkg/datastore. `produce.ProducerFunc[M common.Versioned]` is
  `func(ctx, tx datastore.Tx) (*M, error)`; the datastore's own
  `ProduceFunc` declaration is deleted and it reads produce's.
- SQL owner comments follow the rename: `-- vulkan: consume.getGroup`,
  `-- vulkan: produce.createNextIdPartition` (review G5).
- Codes move to their roots and drop the lowercase name (review G11):
  produce/controller/datastore/{errors,logs}.go (VK0018, VK0033, VK0056,
  VK0057) -> pkg/produce; migrate/controller/datastore/errors.go
  (VK0053) -> pkg/migrate; consumer/logs.go VK0041 -> consume;
  producer/logs.go VK0038 -> produce; systemmanager/logs.go VK0065 ->
  system. pkg/metrics/consumergroup.go (10 metrics) is renamed
  metrics.go. tools/conventions and tools/codeexport then link roots
  only -- the produce/controller/datastore line goes.

### Machine checks (tools/conventions)

- No `type X = ` declaration outside pkg/vulkan/alias.go.
- pkg/vulkan imports no package whose path ends in `/controller` or
  `/datastore`.
- A controller, datastore, batcher, or worker package exports no struct
  other than its Config, its `*Row` structs, its controller / datastore /
  instance / provisioner types, and their constructors.
- The alias closure (review G3): walk every exported signature and field
  of pkg/vulkan with go/types; every named type, `Err*` var, and `Event*`
  var from this module that is reachable must have a same-named vulkan
  alias or var.
- The SQL owner comment's package segment equals the declaring root's
  name (review G5).
- Every `diagnostic.New*` call site is in `pkg/<x>/{errors,logs,metrics}.go`
  or under pkg/common, and the variable it initializes is exported
  (review G11).

### Rule text

- CONVENTIONS ## Package layout: the root line gains "and the declaration
  inputs its controller consumes (TopicConfig, ScheduleConfig,
  ScheduleSpec)" and drops "no Config types"; adds the thing/activity
  naming sentence, the `<Root>Controller` sentence, and the
  one-declaration law; `consumergroup` becomes `consume` in the kinds list
  and the template example; `produce` joins the domain list.
- CONVENTIONS ## Datastores: the sanctioned crossing names
  `datastore.InTransaction` and `datastore.Tx`.
- Decision record: the four laws, amending [0625] ("every user type
  aliased in vulkan from its declaring package") and [0555].
- ROADMAP Now: the "real home" item is replaced by a pointer here.

### Build order

Each step builds and passes `go test -race` on touched packages before the
next. Path moves are `git mv` plus package clause plus import rewrite.

1. Path moves with no signature change: consumergroup -> consume;
   producer/{controller,batcher} -> produce/{controller,batcher};
   Versioned -> common; Tx -> datastore; TopicConfig -> topic;
   ScheduleConfig -> schedule. Update imports in pkg, cmd/vulkan,
   otelvulkan, examples, tools/conventions/conventions.go,
   tools/codeexport. SQL owner comments. `just site-codes` to regenerate
   website/src/data/codes.json.
2. Type renames: ProduceController/ProduceDatastore,
   ConsumeController/ConsumeDatastore (constructors and receivers follow).
3. Signature changes: ScheduleSpec at the controller, scheduler, and
   vulkan; ConsumerConfig.Bindings; admin's five verbs (GetGroup ->
   ListGroupWorkers rename first, then the new GetGroup).
4. Codes to roots (review G11), then the vulkan rewrite: alias.go,
   errors.go, events.go; delete adapter.go and the three config twins and
   schedule_spec.go; client holds assemblers only; producer_instance.go;
   instances hold private fields.
5. Conventions tests, CONVENTIONS/AGENTS text, decision record, ROADMAP
   item slimmed.
6. Docs: website/src/content/docs/guides/handler-outcomes.mdx (imports
   pkg/consumergroup), errors/VK0054.md and VK0055.md
   (`consumergroup.Delay`/`Terminal` -> vulkan), guides/client.mdx and
   guides/schedules.mdx mentions; config doc comments that say "Default:
   text lines to stderr" gain "the client's logger when built through
   vulkan.Client".

Directly affected labs at step 3/4: group-config-lab and binding-lab
(Bindings), schedule-lab and schedule-concurrency-lab (ScheduleSpec),
compaction-lab and compaction-head-write-lab (admin heads), routing-lab.
Full fresh-DB suite at the review-ready checkpoint.

### Review findings (2026-09-03), each resolved by a rule

- **G1 Versioned was placed in topic; five domains read it.** Law 2:
  two-or-more domains means common. Also required: a root imports
  infrastructure only, and produce's ProducerFunc needs the constraint.
  Resolution: `common.Versioned`, `common.SchemaVersionOf`.
- **G2 consumer.ConsumerConfig has no Bindings.** The vulkan twin added
  it and Register took a param. Law 4 forbids the twin; a declaration
  input travels in the declaration config. Resolution: the field on
  ConsumerConfig, the param deleted.
- **G3 The alias list was hand-kept and already incomplete** (Alert,
  Measurement, and everything nested: Owner, OwnerKind, alert.Status,
  alert.Severity, metrics.Kind, metrics.Unit, ConsumerGroupSnapshot,
  SchemaVersionSnapshot, GroupSchemaVersionLag, worker.InstanceTarget,
  schedule.Expression, logging.Logger). Law 4's premise is one import.
  Resolution: the closure test computes the set; alias.go is whatever it
  demands.
- **G4 Errors and events were never re-exported**, so docs and examples
  spell `topic.ErrTopicNotFound` and `consumergroup.Terminal`. Same law.
  Resolution: errors.go, events.go, covered by the closure test; docs
  updated in step 6.
- **G5 SQL owner comments and codes.json name the package.**
  `-- vulkan: consumergroup.getGroup` goes stale on the rename, and the
  sandbox's codes.json is generated from the Go declarations. Resolution:
  rename the comments in step 1, regenerate with `just site-codes`, and
  add the owner-segment check so a future move cannot leave one behind.
- **G6 Controller and datastore type names carried the old noun**
  (ConsumerGroupController, ProducerController). Law 3's
  `<Root>Controller` sentence. Resolution: rename in step 2; worker
  packages keep their names, they are not the root's own.
- **G7 The schedule controller did not parse the cron expression** --
  scheduler did, with the controller taking a parsed Expression. All
  input validation lives at the controller. Resolution: the controller's
  Register takes the spec and parses spec.Cron; scheduler passes it
  through.
- **G8 Group.Get's (nil, nil) on a missing topic** lived in vulkan's own
  composition. Moving the verb to admin must keep the comma-ok shape.
  Resolution: stated on admin.GetGroup above.
- **G9 The ROADMAP Now item points the other way** ("declarations move
  into vulkan"). Resolution: replaced by a pointer to this plan; the
  decision record carries the reversal and why (Go's import graph forbids
  a package that both declares the types and imports the machinery that
  reads them).
- **G10 The CLI builds a TopicController and spells
  `topiccontroller.TopicConfig`** (cli/destroy.go, cli/topic_config.go).
  Not this plan's scope -- the CLI sits under the module tree and may
  reach machinery -- but the import path changes in step 1.
- **G11 Seven codes are declared below or beside a root** (2026-09-03,
  found closing step 1): four in produce's datastore, one in migrate's
  datastore, three in the assemblers consumer, producer, systemmanager --
  all unexported `err*`/`event*` names, which is why alias.go never saw
  them. CONVENTIONS already says "the owning pkg/<x>/errors.go / logs.go";
  law 2 restated for codes makes the root the only home. Resolution: the
  moves listed under Lower-package deltas, the exported name, the
  call-site walk above.
