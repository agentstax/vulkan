---
status: accepted
date: 2026-07-28
phase: "13"
---

# 0374 — Datastore retry policy becomes retry.Policy, carried per config, not a global

**Context.** The retry policy was hardcoded identically in four places — `NewDatastoreRetry(6, time.Second, 5*time.Minute, 2, ...)` in the producer, topic, consumer, and metrics datastores — with no way for users to configure it, and metrics polling inherited a write path's 5-minute backoff ceiling, so a DB blip could make the metrics readout look hung.

**Decision.** `retry.Policy{MaxRetries, BaseDelay, MaxDelay, Exponent}` in `pkg/retry/policy.go`, with `retry.NewDefaultRetryPolicy()` and a pointer-receiver `WithDefaults()` that nil-checks the receiver (a fully unset `*Policy` returns the default outright) and resolves zero fields in place. Each of the four sites' configs (`ProducerDatastoreConfig`, `ConsumerDatastoreConfig`, `ConsumerMetricsDatastoreConfig`, `topic.Config`) gained a `Retry *retry.Policy` field, resolved the same way their `Logger` field already is. `topic.Exists`/`Destroy` (no config param) fall back to the default, mirroring their nil-Logger fallback.

**Consequences.** "Set once" ergonomics come from constructing one `Policy` value and passing it into all four configs; passing a different value into one config diverges it — metrics polling can now take a shorter policy than a real write path. **Rejected:** a global mutable singleton — hidden cross-package coupling for a library.
