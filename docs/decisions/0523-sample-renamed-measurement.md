---
status: accepted
date: 2026-08-16
phase: "14"
---

# 0523 — The metric point type is Measurement, not Sample

**Context.** 0522 named the message type metrics.Sample. Review judged
"sample" monitoring-insider vocabulary a first-time user would not
recognize, and "Metric" alone misnames the struct -- a metric is the named
series, the struct is one dated value of it (the CLI's list shows 36 rows
but only 21 distinct metrics). Surveyed: Prometheus "sample", otel API
"measurement", OTLP/Datadog/Influx/GCM "point", CloudWatch "metric datum".

**Decision.**
- metrics.Sample -> metrics.Measurement (otel's API word, plainest English:
  what was measured, the value, when). NewMeasurement, MeasurementKey.
- Name consts become Metric* (MetricUnclaimedWorkers ...) with
  MetricNameReservedPrefix -- those constants are metric names, not points.
- Ripple renames keep each surface's noun honest: admin ListMeasurements /
  ListMeasurementMessages, otelvulkan RegisterMetricInstruments (one
  instrument per metric name), CLI list footer counts "series".

**Consequences.** User-facing docs never say "sample"; "metric" stays free
to mean the named series. Rejected: keeping Sample (user-vetoed), Metric
(wrong referent, metrics.Metric stutter), Point/MetricPoint (runner-up,
less self-explanatory than Measurement).
