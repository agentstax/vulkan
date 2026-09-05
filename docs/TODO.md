# TODO

Sliding window of in-flight work only. Future work lives in ROADMAP.md;
shipped work in HISTORY.md; decision rationale in DECISIONS.md ->
docs/decisions/.

## Metrics as first-class handles

Build the first `pkg/vulkan` metrics surface from the first Now item in
ROADMAP.md. The first pass covers System, Topic, and Group handles. Consumer,
producer, and possible scheduler instance handles remain Later; custom user
definitions remain parked.

### Settled contract

- `Snapshot(ctx)` computes typed live state from source tables. `Latest(ctx)`
  returns the newest retained collector observation, and `History(ctx, limit)`
  returns retained observations newest first. `Measurement.At` is always
  exposed; `Latest` does not promise freshness.
- Metrics handles expose typed selectors for Vulkan built-ins. Only
  `System().Metrics().Metric(name, attributes)` accepts an arbitrary metric
  name, for user-produced measurements.
- `System().Metrics().Definitions()` returns every Vulkan built-in definition.
  Topic and Group `Definitions()` return their applicable scopes. Definitions
  are available without database I/O and do not include inferred user metrics.
- `Latest` and `History` return bare `Measurement` values. A series with no
  retained point returns `(nil, nil)` from `Latest`; `History` returns an empty
  slice and rejects a non-positive limit.
- Keep `diagnostic.DiagnosticMetric` in `pkg/common/diagnostic` and keep its one
  VK-code/name registry. `pkg/metrics` owns the catalog entries, organized by
  subject rather than in one catalog file. There is no second registry.

### 1. Write and review the public proposal — review pending

- Replace the current metrics examples in
  `website/src/content/docs/guides/client.mdx` with a clearly labeled
  **Proposed** section covering the complete first-pass surface:

  ```go
  systemMetrics := client.System().Metrics()
  definitions := systemMetrics.Definitions()
  latest, err := systemMetrics.Latest(ctx)
  custom := systemMetrics.Metric(name, attributes)

  topicMetrics := client.Topic[Order]("orders").Metrics()
  snapshot, err := topicMetrics.Snapshot(ctx)
  compacted := topicMetrics.Compacted()

  groupMetrics := client.Topic[Order]("orders").Group("billing").Metrics()
  snapshot, err := groupMetrics.Snapshot(ctx)
  backlog := groupMetrics.CursorBacklog()
  point, err := backlog.Latest(ctx)
  history, err := backlog.History(ctx, 20)
  ```

- Show the definition fields (`Code`, `Name`, `Kind`, `Unit`, `Description`,
  `Scope`, `AttributeKeys`), absence behavior, ordering, and the difference
  between live `Snapshot` and collected `Latest` with an explicit stale-time
  example.
- Review that proposal with the user before changing the public API. After the
  surface settles, record the decision in the next numbered decision record;
  supersede only the clauses of [0569] that the richer declaration changes.

### 2. Make the existing registry the complete built-in catalog

- Extend `diagnostic.DiagnosticMetric` with scope and ordered attribute-key
  metadata. Its constructor validates non-empty/known metadata, duplicate
  attribute keys, and the existing unique code/name invariants.
- Add the typed public `MetricDefinition`/`MetricScope` view in `pkg/metrics`.
  Convert registry entries into defensive values so callers cannot mutate
  catalog state through `AttributeKeys`.
- Declare every current Vulkan metric, including the collector gauges that are
  still string constants. Keep declarations beside their subject read models:
  worker, schedule, alert, topic, consumer group, and consumer session.
- Preserve the existing metric wire names, kinds, units, attributes, and VK
  codes. Allocate new VK codes for previously undeclared built-ins; do not add a
  code to `Measurement` on the wire.
- Implement deterministic all-definition and scope-filtered reads over
  `diagnostic.Metrics()`. They perform no I/O and expose Vulkan built-ins only.

### 3. Drive every built-in producer from its declaration

- Change the metrics collector, consumer session producer, and built-in alert
  checks to construct `Measurement` values from their declaration's name,
  kind, and unit. Call sites supply only the observed value, attributes, and
  observation time.
- Validate each built-in measurement's exact attribute-key set against its
  declaration before publishing. Keep user-created measurements open-ended and
  governed by `NewMeasurement`'s existing validation.
- Keep `vulkan explain`, code export, conventions checks, and `otelvulkan`
  description/help lookup on `diagnostic.DiagnosticMetric`; extend their
  rendered/exported metadata rather than creating parallel lookup tables.
- Add a catalog coverage test: every resource-scoped definition appears through
  exactly one typed selector, and every selector resolves the registered
  declaration it claims.

### 4. Reuse and regularize the read paths

- Reuse compaction-head listing for `SystemMetricsHandle.Latest`, exact-key
  compaction-head lookup for `MetricHandle.Latest`, and exact-key message
  listing for `MetricHandle.History`. Unwrap storage envelopes at the Vulkan
  boundary; do not add metric SQL.
- Reuse `metrics/controller.TopicSnapshot` and `ConsumerGroupSnapshot` for live
  `Snapshot` calls. Replace the name-only group fan-out with id-and-name rows,
  then pass that resolved identity through the one group-snapshot computation.
  This removes the current second lookup and gives missing groups a deliberate
  `ErrGroupNotFound` path instead of leaking `pgx.ErrNoRows`.
- Keep ordering explicit: definitions by VK code, system latest measurements by
  measurement key, and history newest first.

### 5. Build the Vulkan handle tree

- Replace `SystemHandle.Measurements` / `Measurement` and
  `TopicHandle.Metrics(ctx)` with no-I/O `Metrics()` handle constructors.
- Add `SystemMetricsHandle`, `TopicMetricsHandle`, `GroupMetricsHandle`, and one
  exact-series `MetricHandle`. A metric handle binds one registered definition
  plus resource attributes, or one arbitrary system name plus attributes; it
  holds no database row.
- Add typed selectors for all System-, Topic-, and ConsumerGroup-scoped built-in
  definitions. Selectors bind required resource attributes centrally and never
  accept an arbitrary name.
- Alias the new user-facing definition/scope types and constants through
  `pkg/vulkan` according to [0643]. Keep admin methods as internal adaptation;
  the documented client tree is the only public path.

### 6. Verify behavior and compatibility

- Unit-test declaration validation, deterministic/scoped definitions, defensive
  attribute slices, selector coverage, measurement-key binding, absence
  behavior, positive history limits, and storage-envelope unwrapping.
- Run `go test -race` for touched packages and modules: `pkg/common/diagnostic`,
  `pkg/metrics/...`, `pkg/admin`, `pkg/vulkan`, `pkg/alert/...`, `otelvulkan`,
  `cmd/vulkan`, and `tools/...` as applicable.
- Update and run the directly affected metrics collector and client examples.
  Run the full fresh-database lab suite only when this work reaches the
  review-ready checkpoint.

### 7. Close the work

- Remove the **Proposed** label only after implementation and verification match
  the reviewed guide exactly.
- At shipment, add the dated HISTORY.md milestone citing the new decision,
  remove this TODO section and the completed first-class-metrics Now item, and
  leave the instance-handle and custom-definition roadmap items in place.
