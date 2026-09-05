---
status: superseded
date: 2026-09-05
phase: pre-v1
---

# First-class metrics use one declaration catalog

Superseded in part by [0648].

## Context

The client needs live resource snapshots, retained metric history, and a
catalog that says which Vulkan metrics exist before collection. The existing
diagnostic registry declares only consumer-session counters; the collector's
other metrics are string constants that repeat kind and unit at production.
[0569] established the right registry but not resource scope, attribute keys,
or catalog growth across subjects.

## Decision

- `diagnostic.DiagnosticMetric` remains the concrete VK-coded declaration in
  the one diagnostic registry. It gains scope and ordered attribute keys;
  kind, unit, and scope remain strings there to preserve the infrastructure
  dependency direction.
- `pkg/metrics` owns every declaration instance and exposes typed
  `MetricDefinition`, `MetricScope`, `MetricKind`, and `MetricUnit` views.
  Definitions are defensive values derived from `diagnostic.Metrics()`, never
  a second registry.
- Metric declarations split by subject into `*_metrics.go` files beside the
  worker, schedule, alert, topic, consumer-group, and consumer-session models.
  One conventions walk still verifies every code and every typed field.
- `System().Metrics()` lists all built-in definitions and all retained series;
  Topic and Group metrics handles list their applicable built-ins and expose
  typed selectors. `System().Metrics().Metric(name, attributes)` is the one
  arbitrary series selector.
- `Snapshot` reads live source tables. `Latest` and `History` return bare
  `Measurement` values from retained collection, always including `At`;
  newest retained does not mean fresh.

## Consequences

- A new built-in metric has one declaration feeding collection, selectors,
  definitions, explain output, and Prometheus help.
- The former gauge-name constants become declaration pointers. A direct
  `pkg/metrics` caller uses `MetricCursorBacklog.Name`; the `pkg/vulkan`
  surface had not exported those constants.
- User metrics appear in retained reads but not definitions until durable user
  declarations settle separately.
- This supersedes [0569]'s five-field metric declaration. Its shared registry,
  plain diagnostic strings, code-free wire measurement, session flush, and
  split live/retained reads remain unchanged.
