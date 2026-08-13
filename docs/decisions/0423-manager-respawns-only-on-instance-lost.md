---
status: accepted
date: 2026-08-08
phase: "14a"
---

# 0423 — The manager respawns only on ErrInstanceLost and propagates every other error

**Context.** The manager's reconcile loop supervises the instances it spawns and needs a policy for what happens when one of them exits with an error.

**Decision.** `ErrInstanceLost` triggers a respawn through the normal reconcile path; any other error from a spawned instance's Run propagates out of the manager.

**Consequences.** Losing a claim (a lease expired under a stall, another process won arbitration) is expected under lease-based liveness and heals by reconciling and re-claiming; it carries no signal that the code is broken. Every other error is treated as a real fault and surfaces to the caller instead of being retried invisibly.
