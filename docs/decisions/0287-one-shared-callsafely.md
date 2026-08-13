---
status: accepted
date: 2026-07-12
phase: "9"
---

# 0287 — One shared callSafely wraps consumerFunc for all three claim paths

**Context.** Panic recovery and the hard per-message timeout both need a goroutine boundary around `consumerFunc`, for different reasons: catching a same-stack panic, and giving the caller an exit door independent of the callee. Three claim paths (`CursorClaim`/`ExceptionClaim`/`LifecycleClaim`) each invoke `consumerFunc`.

**Decision.** One shared helper, `callSafely(ctx, consumerFunc, work, messageID, attempt) error`, called by all three claim paths instead of invoking `consumerFunc` raw. It recovers a panic into an ordinary error (panic value plus `runtime/debug.Stack()`) and races `consumerFunc` in its own goroutine against the hard timeout.

**Consequences.** The panic-recovery `defer` is threaded through the timeout race once, not duplicated per mechanism or per claim path, and the three paths cannot drift apart. **Rejected:** a separate wrapper per failure mode — the recovery `defer` would have to be duplicated inside each, and tripling defer/recover logic across claim paths invites divergence.
