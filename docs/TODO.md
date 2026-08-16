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
- [x] 3. Collector worker, fleet samples. pkg/worker/metricscollector on the
  cronscheduler template, system-scope owner, worker name metrics_collector.
  Declared via the system controller's existing declarer list (joined
  cronscheduler + manager there -- no alertDeclarers change needed).
  Provisioned by SystemManager AND every embedded consumer manager. Poll
  interval in worker metadata (default 30s). Emits the vulkan.worker.state.*
  samples (name consts in pkg/metrics) with CompactionKey + RoutingKey.
  metrics.TopicConfig() gained AllowDropPastCommitted: true and the
  retention-is-history-window comment. The __system.metrics exclusion has
  nothing to exclude until chunk 4's topic/group samples -- lands there.
- [x] 4. Full sample coverage: cron-job samples (vulkan.cron.state.*),
  per-group samples, topic samples. Mapping snapshot verbs to sample names;
  loop shape unchanged. As built: names mirror the dormant otel gauges
  verbatim (3 cron, 14 consumer-group, plus vulkan.topic.state.compacted);
  attributes are group/topic/version (version so two schema versions of one
  topic name never share a key); collector definition gained a
  TopicController for ListTopics; __system.metrics skipped by name in
  collectTopics.
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
  topic -> CLI -> /metrics, full fresh-DB suite. metricslab must exercise a
  full-size collection pass under -race: concurrent collectTopic fan-out
  (TopicConcurrency) plus each group's concurrent produces against one
  ProducerInstance. Alert-pipeline instrumentation stays a ROADMAP
  follow-on.
