---
status: accepted
date: 2026-07-12
phase: "9"
---

# 0288 — The hard per-message timeout is a detached-goroutine race, with abandonment accepted and tracked

**Context.** `context.WithTimeout` alone cannot enforce `WorkTimeout`: a context deadline only takes effect if the callee checks `ctx.Err()`/`ctx.Done()`, and user code inside `consumerFunc` cannot be assumed to. Go has no primitive to kill a goroutine.

**Decision.** Inside `callSafely`, `consumerFunc` runs in its own goroutine sending into a buffered `done` channel, raced against `time.After(WorkTimeout + WorkTimeoutGrace)`. The caller gives up independent of the callee and routes a timeout into the same per-message error path as any other failure.

**Consequences.** A timed-out `consumerFunc`'s goroutine keeps running in the background — contained, not killed. That cost is made visible instead of hidden: each abandoned goroutine is registered in an in-process registry and gauged (recorded separately). The `done` channel is buffered so the late finisher's send never blocks forever. **Rejected:** relying on context cancellation alone — enforceable only for code that cooperates.
