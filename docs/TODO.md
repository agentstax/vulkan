# TODO

Sliding window of in-flight work only. Future work lives in ROADMAP.md;
shipped work in HISTORY.md; decision rationale in DECISIONS.md ->
docs/decisions/.

## Metrics collection and otel exposure ([0522], naming amended by [0523])

- [x] 1. Measurement vocabulary. metrics.Measurement (name, kind, value, unit,
  attributes, at) + Kind consts + NewMeasurement + MeasurementKey in pkg/metrics beside
  AbandonedRoutineKey. Tests pin MeasurementKey determinism (sorted attribute
  keys).
- [x] 2. ListCompactionKeyMessages(ctx, topicId, compactionKey, limit) on
  CompactionController + datastore, sibling to ListCompactionHeads. Newest
  first, limit required. Also: partial (compaction_key, id) index on
  message_log in the baseline DDL, and compaction's HeadData renamed
  MessageData now that heads and history rows share it.
- [x] 3. Collector worker, fleet measurements. pkg/worker/metricscollector on the
  cronscheduler template, system-scope owner, worker name metrics_collector.
  Declared via the system controller's existing declarer list (joined
  cronscheduler + manager there -- no alertDeclarers change needed).
  Provisioned by SystemManager AND every embedded consumer manager. Poll
  interval in worker metadata (default 30s). Emits the vulkan.worker.state.*
  measurements (name consts in pkg/metrics) with CompactionKey + RoutingKey.
  metrics.TopicConfig() gained AllowDropPastCommitted: true and the
  retention-is-history-window comment. The __system.metrics exclusion has
  nothing to exclude until chunk 4's topic/group measurements -- lands there.
- [x] 4. Full measurement coverage: cron-job measurements (vulkan.cron.state.*),
  per-group and topic measurements. Mapping snapshot verbs to metric names;
  loop shape unchanged. As built: names mirror the dormant otel gauges
  verbatim (3 cron, 14 consumer-group, plus vulkan.topic.state.compacted);
  attributes are group/topic/version (version so two schema versions of one
  topic name never share a key); collector definition gained a
  TopicController for ListTopics; __system.metrics skipped by name in
  collectTopics.
- [x] 5. CLI reads. Admin gains a CompactionController[metrics.Measurement]
  sibling to alertHeads (safe: only measurements carry compaction keys on the
  topic). `vulkan metrics list` over heads; `vulkan metrics get` over
  ListCompactionKeyMessages, one block per attribute set, series cap,
  --attribute/--system/--user filters. Mirrors alert_list.go. As built:
  admin verbs ListMeasurements + ListMeasurementMessages(compactionKey, limit);
  list has --system/--user (name-prefix filter) and -q printing series
  keys; get <name> has --attribute (repeatable key=value), --limit 10,
  --series 10 with a truncation notice; measurementValueCell renders by UCUM
  unit (ms -> duration, annotation/none -> bare number, other real units
  verbatim beside the number).
- [x] 6. Otel module extraction. New nested module (otelvulkan/, joins
  go.work with no require line like cmd/vulkan; release tagging becomes a
  three-module story -- update cmd/vulkan/go.mod's note). pkg/metrics/metrics
  moves and reshapes into the head-driven bridge (Float64ObservableGauge by
  metric name -- instruments can't be created inside a callback, so new names
  need a registration pass) + Prometheus exporter + Handler(). Core go.mod
  drops otel/prometheus entirely; CONVENTIONS dependency list updates.
  As built: two layers (user-directed). otelvulkan.Metrics (NewMetrics(ds,
  cfg); user renamed from Meter) registers the metric instruments on otel
  meters -- MetricsConfig.Meter takes the user's own (default: the global
  otel provider's), so their own readers/pipeline collect the data;
  RegisterMetricInstruments lists heads and creates a gauge -- or
  observable counter for KindCounter -- per unseen name per registered
  meter, replacing that meter's callback registration, and the observation
  callback lists heads again under CollectTimeout (default 5s), observing
  by name with attributes as labels. Metrics holds exactly ONE meter (the
  ecosystem norm; a shared-*Metrics NewExporter signature was tried and
  reverted -- it forced a registered-meter list because readers attach at
  provider construction). otelvulkan.Exporter (NewExporter(ds, cfg)) owns a
  private Metrics bound to its provider's meter, with the otel Prometheus
  reader as that provider's only reader; Handler() runs the registration
  pass per scrape then serves (delegated RegisterMetricInstruments is
  public for fail-fast startup); scope-info labels dropped
  (WithoutScopeInfo).
  pkg/metrics/metrics deleted outright (nothing constructed it).
- [x] 6.5. User measurement producer/consumer (item 1 of the original module
  scope, restored; user renamed Sample* -> Metrics*).
  otelvulkan.MetricsProducer (core producer.ProducerConfig passed through;
  Register(ctx) pins __system.metrics v1) whose instance Produce(ctx,
  measurement) sets RoutingKey = name and CompactionKey = MeasurementKey(name,
  attributes), rejecting the reserved prefix (new
  metrics.MetricNameReservedPrefix const, also used by the CLI --system/
  --user filter). otelvulkan.MetricsConsumer wraps
  consumer.Consumer[metrics.Measurement]; Register(ctx, group, names) uses
  metric names as the binding set (measurements route under their metric
  name), nil = every metric, returning the core ConsumerInstance.
- [ ] 7. Dogfood + checkpoint. `vulkan manager run --metrics-address`
  (cmd/vulkan imports the otel module), metricslab driving collector ->
  topic -> CLI -> /metrics, full fresh-DB suite. metricslab must exercise a
  full-size collection pass under -race: concurrent collectTopic fan-out
  (TopicConcurrency) plus each group's concurrent produces against one
  ProducerInstance. Alert-pipeline instrumentation stays a ROADMAP
  follow-on.
