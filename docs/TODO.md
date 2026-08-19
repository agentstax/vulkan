# TODO

Sliding window of in-flight work only. Future work lives in ROADMAP.md;
shipped work in HISTORY.md; decision rationale in DECISIONS.md ->
docs/decisions/.

## Worker-tier surface review (from ROADMAP Now; started 2026-08-19)

Everything the worker tier exports postdates Phase 13's painstaking pass.
Same rigor: naming, signature shape, required-vs-optional split, what
should be unexported, doc comments. The reading doubles as the internal
readability-debt sweep -- file long-func/shaping-wad findings as they
surface.

Surfaces, in review order:

- [x] pkg/worker vocabulary -- reviewed 2026-08-19. Worker.Owner made
      *common.Owner (user-settled; was the lone by-value Owner). Metadata
      `any` is the deliberate JSONB contract (ParseMetadata types it).
      Declarer/Provisioner/Definition/Execution restructure + *Execution ->
      *Instance rename deferred to the naming pass by design.
- [ ] pkg/worker/controller -- WorkerController + WorkerConfig +
      ControllerConfig, free funcs (ParseMetadata, RegisterInstance,
      ValidateOwner), InstanceRunner/InstanceTickRunner + configs
      (MetadataValue no longer exists -- stale roadmap mention, removed)
- [ ] worker kinds -- janitor, waterline, cronscheduler, metricscollector
      (Definition/Execution/Config each), manager (+ Runner, RunnerConfig)
- [ ] pkg/systemmanager -- SystemManager + SystemManagerConfig
- [ ] consumer split -- Consumer/ConsumerInstance/ConsumerFunc +
      ConsumerConfig; the three sub-consumer Definitions + Configs;
      consumer/base (BaseConsumer/BaseDefinition/BaseExecution + configs).
      The planted trap: constructors of bare pieces (NewMessageConsumerDefinition
      et al) changed meaning while keeping shape -- decide whether they
      should be harder to reach by accident.
- [ ] pkg/producer -- Producer/ProducerInstance, ProduceItem/ProduceResult,
      ProduceOptions/ProducerFunc/TransactionFunc/Tx aliases, InTransaction
- [ ] CLI -- `vulkan manager` commands (manager.go, manager_run.go)

Findings accumulate here as sub-bullets per surface; decisions get records;
edits land per-surface with build + targeted tests.
