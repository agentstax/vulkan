---
status: accepted
date: 2026-06-15
phase: "2"
---

# 0047 — Delivery is at-least-once; consumerFunc must be idempotent

**Context.** Lease reclamation means the same message can be processed twice:
a worker that runs past its lease looks crashed and gets reclaimed, and a
worker that crashes after its side effect but before `RecordSuccess` re-runs
regardless of lease length. This was demonstrated deliberately (a sleep longer
than the lease produced a double-processing).

**Decision.** At-least-once is the stated contract. `consumerFunc` is
documented as "ideally idempotent func", and the lease window is kept
comfortably longer than the work timeout so live workers are rarely reclaimed.

**Consequences.** Idempotency is mandatory for correctness — a longer lease
only lowers how often double-delivery happens and can never eliminate it. The
two mitigations are complementary, not alternatives: the timeout buffer
controls frequency, idempotency makes the residual deliveries harmless.
