---
status: accepted
date: 2026-08-18
phase: 14b
---

# The metrics domain's write door is pkg/metrics/producer

## Context

pkg/consumer/metrics held the metrics event producer (a goroutine + a
producer.Producer publishing abandoned/cleared GoRoutineEvents to
__system.metrics) and the GoRoutineEvent message shape. That put the
metrics domain's write door inside the consumer tree: pkg/consumer/base
imported all of producer transitively just to call Add/Remove, and the
consumermetrics-vs-metrics name collision forced aliases everywhere. The
package was also the tree's only config without WithDefaults/Validate,
had no ds nil-check, a nil-safe enqueue receiver, a reader-less Noop
field, and unused ctx params on Add/Remove.

## Decision

- GoRoutineEvent + NewGoRoutineEvent move to pkg/metrics vocabulary
  (goroutineevent.go) beside EventType and AbandonedRoutineKey.
- The producer moves to pkg/metrics/producer, named by the domain-layer
  pattern the controller half already follows: MetricsProducer in
  producer.go parallels MetricsController in controller.go, ProducerConfig
  in producer_config.go parallels ControllerConfig in controller_config.go.
  Inside, pkg/producer imports as iProducer (the i* collision alias);
  callers import metricsproducer (the <domain><layer> alias convention).
- The arrow is the alert pattern -- the domain's write door owns the
  producer, consumers use it (AlertController is the template). Hiding
  base's transitive producer dependency behind an interface seam was
  rejected: the coupling is real, the seam would be decoration.
- Fixed in the move: ProducerConfig gains WithDefaults/Validate;
  NewMetricsProducer nil-checks ds and resolves cfg; the nil-safe enqueue
  receiver, the Noop field, and the Add/Remove ctx params are deleted
  (abandonedEvents is nil-checked at NewBaseDefinition, so the receiver
  guard was dead).

## Consequences

- pkg/consumer/metrics is deleted.
- Add/Remove are fire-and-forget queue writes with no ctx, matching what
  they always did.
- A domain needing a write door gets a producer/ layer package beside
  controller/, named by the same pattern.
