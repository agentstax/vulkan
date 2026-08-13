---
status: accepted
date: 2026-07-12
phase: "9"
---

# 0293 — The abandoned-goroutine registry is a mutex-guarded map keyed by (MessageId, Attempt), kept as plain in-process state

**Context.** Timed-out `consumerFunc` goroutines cannot be killed, only tracked. The registry needed a key, a synchronization choice, and a decision on how formal to make the metrics before any metrics-library commitment existed.

**Decision.** `ConsumerMetrics.AbandonedRoutines` is a `map[abandonedKey]time.Time` keyed by `(MessageId, Attempt)` — a message's first and second attempts can each leave a live abandoned goroutine, so message id alone would let one entry overwrite the other. It is guarded by a `sync.Mutex`, with `TrackingTotal()` (atomic counter), `CurrentTotal()` (gauge from map length), and `ReclaimLatency()` (average over a self-locking `ConcurrentBoundedRingBuffer[time.Duration]`, capacity 256, `Values()` returning a defensive copy). The shape is deliberately informal — no metrics-library commitment yet; wiring these already-built numbers into a pluggable interface is deferred until one is chosen.

**Consequences.** Labs can assert against a live gauge immediately; a later metrics layer inherits working numbers instead of a one-off shape invented early. **Rejected:** `sync.Map` — the access pattern is one Add/Remove/CurrentTotal call each per event, not enough concurrent contention to justify its striped-lock complexity. **Rejected:** keying by message alone — under-keys when multiple attempts of the same message are simultaneously abandoned.
