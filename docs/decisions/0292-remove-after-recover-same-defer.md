---
status: accepted
date: 2026-07-12
phase: "9"
---

# 0292 — AbandonedRoutines.Remove runs after recover() within the same defer

**Context.** The spawned goroutine's single `defer` both recovers panics and removes the goroutine's registry entry. Ordering matters: if `Remove` ran first and itself panicked (for example on a nil `Metrics`), `recover()` would never run, undoing the panic-recovery guarantee entirely.

**Decision.** `recover()` executes first, then `AbandonedRoutines.Remove`, inside the same `defer`.

**Consequences.** A registry failure can never mask a `consumerFunc` panic. **Rejected:** a nil-guard on `Metrics` inside `callSafely`, and a second nested `defer`/`recover` around just the `Remove` call — `validate()` already guarantees `Metrics != nil` before a `WorkConsumer` can be used, and the convention is to trust that guarantee once, at construction, rather than re-checking it at every downstream call site.
