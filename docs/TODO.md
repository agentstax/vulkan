# TODO

Sliding window of in-flight work only. Future work lives in ROADMAP.md;
shipped work in HISTORY.md; decision rationale in DECISIONS.md ->
docs/decisions/.

## Register returns what you run, the client holds the assemblers

Picked up 2026-09-03 from the [0643] close-out review. `RegisterSchedule`
returns a handle where `RegisterProducer` / `RegisterConsumer` return an
instance, and those two fill `Logger` / `Retry` on a per-call assembler
where the scheduler is built once. Two moves, one law each. Decision
record when accepted (next number after 0643): the first amends [0625]'s
"RegisterSchedule returns its handle" and [0621]'s instance clause; the
second restores [0625]'s "ambient config is held once and no resource
config carries Logger or Retry", which [0643]'s alias pass had undone by
aliasing the assembler configs straight through.

### The asymmetry today

| | RegisterProducer / RegisterConsumer | RegisterSchedule |
| --- | --- | --- |
| returns | a runnable instance, generic in Message | `*Scheduler` handle; the scheduler's instance is dropped |
| run verb on | the instance | the handle, beside the admin verbs |
| assembler built | per call, `producer.NewProducer(c.ds, cfg)` | once, in NewClient |
| cfg carries | declaration + Logger/Retry, client fills the nil ones | declaration only |
| interface | `Producer[M]`, `Consumer[M]` | none |

The identity shape (`*ScheduleSpec` vs two strings) stays: Cron is
required and a Config holds only optional fields.

### The laws

1. **Register returns what you run; the handle is what you administer.**
   `Register<Noun>[M]` returns `*<Noun>Instance[M]` carrying the run verb
   and nothing else; `client.<Noun>(name)` is the admin handle. A resource
   is reachable both ways (a consumer group already is: the instance from
   RegisterConsumer, the handle from `Topic.Group`).
2. **The client holds every assembler once.** An assembler's config is
   `<Assembler>Config{Logger, Retry}` (SchedulerConfig is the template);
   the config a Register verb takes is the resource's declaration and
   carries no Logger or Retry. Through the client, every Register verb is
   a pass-through.

### Move 1 -- SchedulerInstance

- pkg/vulkan/scheduler_instance.go: `SchedulerInstance[Message]{instance
  *scheduler.SchedulerInstance[Message]}`, `newSchedulerInstance`, and
  `Schedule(ctx) error` passing through -- the scheduler's own Schedule
  builds a SystemManager over the client's Logger/Retry, so behavior is
  the handle's today. Not gated by `DisableManager`: an explicit run, like
  RunManager.
- `RegisterSchedule[M]` returns `*SchedulerInstance[M]`; the `Scheduler`
  handle loses `Schedule(ctx)` and keeps Get / Suspend / Unsuspend / Run /
  Status / ListMessages / Destroy.
- Callers: examples/phase_1/scheduleconcurrencylab (`nightly.Get` ->
  `client.Scheduler(spec.Name).Get`); site samples in guides/schedules.mdx,
  guides/client.mdx, guides/consumer-group-config.mdx already call only
  `Schedule(ctx)` on the returned value -- verify, then reword the prose
  that says "returns its handle".
- The closure test recomputes the alias set: `scheduler.SchedulerInstance`
  stays private to the wrapper and is not reachable.

### Move 2 -- the ambient config held once

Config split, both stacks the same way:

- `consumer.ConsumerConfig` becomes `{Logger, Retry}` -- the assembler's
  own. The declaration moves to the consume root as `consume.GroupConfig`
  (Message, MessageMin, MessageMax, ConcurrencyOverride, Start, Bindings,
  ExceptionInitialBackoff, MaxRangeReclaims; WithDefaults, Validate,
  DeepCopy move with it) -- the root holding the declaration inputs its
  machinery consumes, the TopicConfig rule; every field is stored on the
  group's worker rows or its cursor. `Consumer.Register[M](ctx, group,
  topic, cfg *consume.GroupConfig)`; the instance holds the resolved
  GroupConfig where it held ConsumerConfig.
- `producer.ProducerConfig` becomes `{Logger, Retry}`. The per-instance
  tuning (Message, Batch, SlowProduceThreshold) becomes
  `producer.ProducerInstanceConfig` in the assembler: a producer has no
  row, and its lowest reader is ProducerInstance.
  `Producer.Register[M](ctx, topic, cfg *ProducerInstanceConfig)`.
- vulkan: NewClient builds `producer.Producer` and `consumer.Consumer`
  once, beside the scheduler; `RegisterConsumer[M](ctx, group, topic, cfg
  *GroupConfig)` and `RegisterProducer[M](ctx, topic, cfg
  *ProducerInstanceConfig)` are pass-throughs; the two nil-fill `if`
  blocks go. Aliases follow the closure test: `GroupConfig` and
  `ProducerInstanceConfig` in, `ConsumerConfig` / `ProducerConfig` out
  (a client user never spells an assembler config). The step 6 comment
  note "the client's logger when built through vulkan.Client" is deleted
  with the fields.
- Layered callers move Bindings and friends from the constructor to
  Register: pkg/alert/{partitioncount,compactionreadcost,workerliveness},
  pkg/metrics/collector, pkg/schedule/producer, pkg/admin (schedule run
  producer), otelvulkan metrics_consumer.go / metrics_producer.go,
  examples/phase_1/alertlab, tools/compat/main.go (already behind step
  3's Register signature -- it builds against the working tree only at
  compat checkpoints).
- Docs: guides/consumer-group-config.mdx, guides/client.mdx, and every
  page or example spelling `ConsumerConfig` / `ProducerConfig` (30 files
  under website/src/content and examples) take the new names.

Naming, the one open choice: `GroupConfig` pairs with `Group` the way
TopicConfig/Topic and ScheduleConfig/Schedule already do, and
`ProducerInstanceConfig` names the struct it configures because there is
no row to name it after. The alternative -- keep `ConsumerConfig` /
`ProducerConfig` as the Register-time names and rename the assembler
configs -- has no name for the assembler config that is not a coinage,
and breaks `<Struct>Config` (SchedulerConfig, SystemManagerConfig,
MessageAdminConfig, ClientConfig all name their struct).

### Machine checks

None new. The closure test recomputes the aliases; `TestMachineryDeclares
NothingUserSpelled` holds because GroupConfig lands at the root and
ProducerInstanceConfig in an assembler; the ## Constructors & configs
rule (a Config holds only optional fields) holds for all four configs.

### Rule text

- CONVENTIONS ## Package layout, API package line: "an assembler's config
  is `<Assembler>Config{Logger, Retry}`; the config a Register verb takes
  is the resource's declaration and carries neither" and "Register returns
  the instance, `client.<Noun>(name)` the handle".
- ## Constructors & configs: the ambient tail sentence gains "only an
  assembler's or a machinery config carries Logger and Retry; a
  declaration config never does".

### Build order

1. Move 1: scheduler_instance.go, scheduler.go handle, RegisterSchedule
   return, handle loses Schedule(ctx), scheduleconcurrencylab, site prose.
   Labs: schedule-lab, schedule-concurrency-lab.
2. Move 2, consumer: consume.GroupConfig, ConsumerConfig slimmed,
   Register signature, the layered callers, vulkan pass-through and
   aliases. Labs: group-config-lab, binding-lab, alert-lab, metrics-lab,
   metrics-collector-lab, manager-autorun-lab.
3. Move 2, producer: ProducerInstanceConfig, ProducerConfig slimmed,
   Register signature, callers, vulkan. Labs: producer-batch-lab,
   create-ahead-lab, alert-lab.
4. Docs (30 files), CONVENTIONS text, decision record, `just site-codes`
   unaffected (no code declarations move). `just verify`, full fresh-DB
   suite at the review-ready checkpoint.
