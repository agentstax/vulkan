---
status: accepted
date: 2026-07-12
phase: "9"
---

# 0281 — Every consumerFunc failure shape lands in the existing per-message retry/backoff/dead-letter path

**Context.** Only one of `consumerFunc`'s four failure shapes was handled: an ordinary returned error, routed through the retry/backoff/dead-letter exception window. A panic crashed the whole claimed range and repeated forever on reclaim (lease expires, identical range re-read, panics again); a hang blocked the batch with no way to give up, since Go has no goroutine kill; a transient datastore error propagated up through `Process`'s poll loop and killed the consumer outright.

**Decision.** Each failure shape is converted to land exactly where the ordinary-error path already lands — one message dead-lettered or retried, never the whole range. A panic is recovered into an ordinary error; a hang is cut off by a hard per-message timeout; a datastore blip is retried invisibly below the claim paths. No bespoke recovery mechanism per failure shape.

**Consequences.** Blast radius is one message for all in-process failures. OS-level faults (stack overflow, SIGSEGV via cgo, OOM-kill, external kill) are not catchable and still rely on the range-level quarantine as backstop. Go's missing goroutine-kill primitive means a timed-out `consumerFunc` is contained, not stopped — its abandoned goroutine keeps running and is tracked, not eliminated. **Rejected:** a separate recovery mechanism per failure shape — three mechanisms to keep consistent instead of one path.
