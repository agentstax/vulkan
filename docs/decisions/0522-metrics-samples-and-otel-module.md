---
status: accepted
date: 2026-08-16
phase: "14"
---

# 0522 — Metrics are samples on __system.metrics written by a collector worker; otel exposure moves to its own module

**Context.** Nothing constructs pkg/metrics/metrics.Metrics, so every
Register*Metric gauge is dormant. Two gaps sit behind that. Vulkan is
brokerless, so fleet-scope facts (unclaimed workers, overdue cron jobs) have no
single reporter -- every process exporting them means duplicate series and N
copies of the same fleet query. And a user who does not run an otel pipeline
gets no metric history at all, even though the snapshot verbs already exist and
__system.metrics is already registered.

**Decision.**
- Collection lives in core; exposure lives in a separate module with its own
  go.mod, the sanctioned shape cmd/vulkan already uses. pkg/metrics/metrics
  moves there and core loses the otel API and noop dependencies; pkg/metrics,
  its controller and its datastore stay. History is a Vulkan feature, not an
  otel feature.
- A collector worker is the single owner of collection. A worker, not a cron
  job: intervals are sub-minute, and suspend, failure streaks and backoff come
  from machinery that exists. Its claim is the leader election a brokerless
  system otherwise lacks. It excludes __system.metrics from its own snapshots,
  or measuring the topic feeds it.
- The message is metrics.Sample -- name, kind, value, unit, attributes, at.
  Prometheus, otel and StatsD already agree on the shape of a metric point;
  inventing a Vulkan-specific one is a second vocabulary for a solved concept.
  Users produce the same struct, so their metrics reach the same bridge,
  handler and CLI with no second path.
- CompactionKey is SampleKey(name, attributes), deterministic on sorted
  attribute keys. Compaction supersedes for delivery without deleting, so heads
  give the current value and the retained log gives history from one topic.
  RetentionTTL is the only knob; AllowDropPastCommitted is true.
- The otel bridge registers one observable gauge per sample name and reads
  compaction heads inside the collection callback. The Prometheus exporter is
  itself a metric.Reader, so the /metrics handler is the meter dumped -- no
  cache behind it. Callbacks run inside the scrape, so the callback ctx carries
  an explicit bound.
- The `vulkan.` name prefix is reserved for the collector. System-versus-user
  is a display filter, not a mechanism.
- The CLI reads heads for current values through the existing
  ListCompactionHeads. History gets ListCompactionKeyMessages(ctx, topicId,
  compactionKey, limit) on CompactionController -- public and general to any
  compacted topic, newest first, limit required because an unbounded read spans
  the whole retention window. `metrics get` lists one block per attribute set,
  capped by a series limit with --attribute to narrow.

**Consequences.** A user's own metric appears on /metrics and in the CLI
without writing an exporter, because a Sample is self-describing -- unit is
what lets the renderer print a duration rather than a bare integer. Cardinality
becomes user-controllable; retention and AllowDropPastCommitted bound that to
disk rather than to correctness. `metrics get` is one head list plus one read
per series, which is why the series cap is not optional. A metric that stops
reporting keeps its head until retention drops it, so the per-series age stamp
is what separates stale from current. Scrape latency becomes database latency.
Per-group gauge registration stops being a question: the collector emits
per-group samples and the bridge registers by name. Rejected: typed per-kind
snapshot messages, which keep history structured and user cardinality out of
the system topic but need one bridge adapter per kind plus a second path for
user metrics; leaving user production as a documented pattern over an ordinary
topic, which is the smallest surface but never reaches the handler; an
in-memory latest-value map behind the handler, unnecessary once head reads are
cheap and it adds a cold start; and publishing only events while re-deriving
state per scrape, which restores the duplicate-query problem and leaves
non-otel users with no history.
