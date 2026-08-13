---
status: accepted
date: 2026-07-14
phase: "10"
---

# 0302 — pkg/ depends on the OTel metric API only, never the SDK or an exporter

**Context.** The metrics integration needs to expose gauges and counters, but importing the OTel SDK or a vendor exporter (Prometheus, Datadog) into the library would add heavy transitive dependencies and pick a vendor on behalf of every consumer.

**Decision.** The integration accepts a `metric.Meter` from `go.opentelemetry.io/otel/metric` — the API package only — defaulting to the global no-op provider, so an unconfigured caller pays zero cost. Each DB-truth number gets an `ObservableGauge` whose callback re-runs the queue-state query; the in-process `AbandonedRoutines` numbers use `Counter`/`UpDownCounter`. `otelexportlab` is the one place in the entire repo that imports `otel/sdk` or a concrete exporter, by design. Precedent: River's `rivercontrib/otelriver` — API-only in the core module, vendor integration outside it.

**Consequences.** Callers connect Prometheus, Datadog, or nothing; `pkg/` never knows which. One gotcha the export lab surfaced: `ObservableGauge`s report every collection cycle via their callback, but `Counter`/`UpDownCounter`/`Histogram` emit no data point until something calls `.Add()`/`.Record()` — a lab must actually induce each event (here, a hard-timeout abandon) before the instrument appears on the wire. All 13 instruments were confirmed present on a scraped Prometheus body.
