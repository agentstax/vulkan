# TODO

Sliding window of in-flight work only. Future work lives in ROADMAP.md;
shipped work in HISTORY.md; decision rationale in DECISIONS.md ->
docs/decisions/.

## Instrument the alert pipeline ([0524], follows [0522]/[0523])

- [x] 1. Alert state gauges (collector-side). New vulkan.alert.state.* name
  consts in pkg/metrics. The collector definition gains a
  CompactionController[alert.Alert] (the same head read admin's alertHeads
  does); a collectAlerts step in the pass counts heads on __system.alerts
  by status/severity into gauges -- exact metric/attribute split (per alert
  name? severity as attribute?) decided against the head shape at build.
  No handler changes; __system.alerts stays a normal topic in
  collectTopics. As built: fleet-level MetricActiveAlerts /
  MetricResolvedAlerts with nil attributes, always produced -- per-name or
  per-severity attributes would mint series enumerated from the heads
  themselves, which go stale when heads sweep out of retention or a
  severity transitions; per-alert visibility comes from chunk 3, where
  the producing job owns its series. collectAlerts sits between
  collectCronJobs and collectTopics, resolving the alerts topic per pass
  like admin does.
- [x] 2. Record outcome return. AlertController.Record returns a named
  outcome (published / resolved / suppressed -- classify already knows
  which arm it took) plus error, so callers can count without a second
  head read. Both alert executions ripple; behavior unchanged. As built:
  RecordOutcome (active | resolved | nothing) beside Record; alertlab's
  classify-arm section now asserts the outcome of every arm.
- [x] 3. Check-run summary (handler-side). New vulkan.alert.check.* consts:
  topics_evaluated, topics_failed, published, resolved; attributes
  {alert: <job name>}. Each alert definition gains a
  producer.Producer[metrics.Measurement], registered per claimed life like
  the alertProducer; evaluateTopics counts its loop and produces the
  summary at the end of every run, INCLUDING runs whose joined error is
  non-nil (per-run gauge + retained history, no cumulative counters, no
  per-failed-topic series -- [0524]). Leaning: a failed summary produce
  joins errs (the collector's "a failed produce fails the pass"
  precedent) -- confirm at build. As built: consts MetricCheckTopicsEvaluated/
  TopicsFailed/PublishedAlerts/ResolvedAlerts; the attribute value is the
  alert's own name (partition_count), not the job name -- alert=
  alert.partition_count would stutter; produceCheckSummary duplicated per
  side in each execution, construction loop then concurrent errgroup
  produces sharing the batcher's transactions (collectConsumerGroup
  style, user-directed); a failed summary produce joins errs, as leaned.
- [x] 4. Checkpoint. alertlab (or a sibling step in it) asserts the state
  gauges and a run summary incl. the partial-failure case; CLI and
  otelvulkan need nothing (names appear via the registration pass).
  Fresh-DB suite at review-ready. As built: alertlab reads the summary
  heads through admin.ListMeasurements (readCheckSummary) after the
  threshold-1 run (published == evaluated, none failed), the quiet run
  (zero publishes), the corrupted-head run (summary EXISTS with
  topics_failed = 1 -- resolved unasserted there, an automatic retry can
  overwrite it), and the retry (0 failed, only the fixed owner resolved);
  metricscollectorlab's coverage set gained the two alert state gauges.
  Fresh-DB suite 36/36 on the final tree.
