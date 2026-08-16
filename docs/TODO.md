# TODO

Sliding window of in-flight work only. Future work lives in ROADMAP.md;
shipped work in HISTORY.md; decision rationale in DECISIONS.md ->
docs/decisions/.

## Metrics collection and otel exposure ([0522])

- [x] 1. Sample vocabulary. metrics.Sample (name, kind, value, unit,
  attributes, at) + Kind consts + NewSample + SampleKey in pkg/metrics beside
  AbandonedRoutineKey. Tests pin SampleKey determinism (sorted attribute
  keys).
- [x] 2. ListCompactionKeyMessages(ctx, topicId, compactionKey, limit) on
  CompactionController + datastore, sibling to ListCompactionHeads. Newest
  first, limit required. Also: partial (compaction_key, id) index on
  message_log in the baseline DDL, and compaction's HeadData renamed
  MessageData now that heads and history rows share it.
- [ ] 3. Collector worker, fleet samples. pkg/worker/metricscollector on the
  waterline template, system-scope owner, worker name metrics_collector.
  Declared in RegisterSystem -- alertDeclarers generalizes to systemDeclarers
  rather than a second loop. Provisioned by SystemManager AND every embedded
  consumer manager. Poll interval in worker metadata. Emits the
  vulkan.worker.state.* samples with CompactionKey: SampleKey(...).
  metrics.TopicConfig() gains AllowDropPastCommitted: true, and its
  RetentionTTL comment states the new semantic: retention = history depth =
  how long a dead series lingers. Collector excludes __system.metrics:
  no topic-level samples for it, no per-group samples for groups on it;
  other __system. topics stay included.
- [ ] 4. Full sample coverage: cron-job samples (vulkan.cron.state.*),
  per-group samples, topic samples. Mapping snapshot verbs to sample names;
  loop shape unchanged.
- [ ] 5. CLI reads. Admin gains a CompactionController[metrics.Sample]
  sibling to alertHeads (safe: only samples carry compaction keys on the
  topic). `vulkan metrics list` over heads; `vulkan metrics get` over
  ListCompactionKeyMessages, one block per attribute set, series cap,
  --attribute/--system/--user filters. Mirrors alert_list.go.
- [ ] 6. Otel module extraction. New nested module (otelvulkan/, joins
  go.work with no require line like cmd/vulkan; release tagging becomes a
  three-module story -- update cmd/vulkan/go.mod's note). pkg/metrics/metrics
  moves and reshapes into the head-driven bridge (Float64ObservableGauge by
  sample name -- instruments can't be created inside a callback, so new names
  need a registration pass) + Prometheus exporter + Handler(). Core go.mod
  drops otel/prometheus entirely; CONVENTIONS dependency list updates.
- [ ] 7. Dogfood + checkpoint. `vulkan manager run --metrics-address`
  (cmd/vulkan imports the otel module), metricslab driving collector ->
  topic -> CLI -> /metrics, full fresh-DB suite. Alert-pipeline
  instrumentation stays a ROADMAP follow-on.
