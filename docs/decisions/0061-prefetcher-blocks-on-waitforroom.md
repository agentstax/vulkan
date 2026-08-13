---
status: accepted
date: 2026-06-20
phase: "3"
---

# 0061 — The prefetcher blocks on WaitForRoom; the buffer never drops a claimed row

**Context.** The consumer pipeline has one Prefetch goroutine batch-claiming rows into a bounded PressureQueue, and a Dispatch loop draining it into one goroutine per message gated by WorkerPoolLimiter. A bounded buffer needs a policy for what happens when it fills.

**Decision.** Prefetch gates on `WaitForRoom` before claiming, replacing the planned `CanEnQueue` check. Backpressure flows backwards: a full buffer stops claiming, so the `EnQueue` drop path is never reached.

**Consequences.** Every buffered row is already claimed — its `lease_until` is stamped and `attempts` incremented — so dropping one would strand a leased row until reclaim. Gating before the claim makes that impossible by construction. The claim size becomes `min(batchLimit, freeSlots)`, tying supply directly to available worker capacity. **Rejected:** check-then-drop via `CanEnQueue`/`EnQueue` — you cannot drop a row you have already leased.
