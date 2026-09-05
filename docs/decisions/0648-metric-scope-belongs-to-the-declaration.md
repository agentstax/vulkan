---
status: accepted
date: 2026-09-05
phase: pre-v1
---

# Metric scope belongs to the diagnostic declaration

Supersedes [0647]'s scope ownership and representation clauses only.

## Context

[0647] put the typed `MetricScope` vocabulary in `pkg/metrics` while
`DiagnosticMetric.Scope` stored plain text. Building a definition therefore
required a cast, and a declaration could carry an unknown scope until a
separate conventions test found it.

## Decision

- `MetricScope` and Vulkan's built-in scope constants live in
  `common/diagnostic` beside `DiagnosticMetric`.
- `DiagnosticMetric.Scope` and `NewDiagnosticMetric` use `MetricScope`.
  Construction rejects an unknown scope.
- `metrics.MetricDefinition.Scope` uses `diagnostic.MetricScope` directly.
  The future `pkg/vulkan` surface aliases that declaration like its other
  reachable exported vocabulary.
- Kind and unit remain strings in the diagnostic declaration because their
  behavior and validation belong to the metrics domain.

## Consequences

- A registered metric cannot carry an invalid scope, and definition conversion
  needs neither a cast nor a second scope type.
- [0647]'s registry, catalog ownership, handles, and read semantics remain in
  force.
