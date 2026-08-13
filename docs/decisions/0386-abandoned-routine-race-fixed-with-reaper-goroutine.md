---
status: accepted
date: 2026-08-01
phase: "14"
---

# 0386 — The abandoned-routine Add/Remove race is fixed structurally with a reaper goroutine

**Context.** In `callSafely`, the `select` could commit to the timeout branch while consumerFunc finished in the gap before `Add` executed: the worker's deferred `Remove` no-ops on the not-yet-inserted key, then `Add` inserts an entry no one will ever remove — the otel `outstanding` counter climbs by one forever. This was the one true unbounded-growth path in the abandoned-routines map.

**Decision.** The worker goroutine no longer touches metrics at all. The timeout branch does `Add` and then spawns a reaper goroutine that blocks on the buffered `done` channel and does the `Remove` when the zombie finally returns. `Remove` cannot precede `Add` because the reaper does not exist until after `Add` — the ordering is program order, not an interleaving argument.

**Consequences.** The reaper lives exactly as long as the leaked goroutine it watches, so it adds nothing unbounded. **Rejected:** a CAS handshake between worker and timeout branch — the first proposal, judged wonky next to the structural ordering. **Rejected:** a "last look" non-blocking re-check of `done` after the timer fires — it defends a nanoseconds-wide window whose failure mode (record failure, redeliver) is already within the at-least-once contract, the rescued result is almost always `ctx.Err()` anyway, and the reaper keeps metrics consistent without it (instant Add-then-Remove, no drift). Live-verified via faultisolationlab under `-race` (which also exposed and fixed a pre-existing data race in the lab's own counter; the library change is race-clean).
