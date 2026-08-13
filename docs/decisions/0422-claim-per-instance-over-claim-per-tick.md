---
status: accepted
date: 2026-08-08
phase: "14a"
---

# 0422 — Exclusivity is claim-per-instance, not claim-per-tick

**Context.** The old duty table enforced exclusivity claim-per-tick: a conditional UPDATE raced on every interval, so the pacing of the loop and the ownership of the loop were the same mechanism.

**Decision.** A worker claims its `worker_instance` slot once at Register; the heartbeat-renewed `expires_at` holds it from then on. The heartbeat IS the exclusivity. Tick pacing becomes the worker's own internal business.

**Consequences.** Ownership arbitration happens once per instance lifetime instead of once per tick. The cost is lease-shaped failover: a dead instance's slot stays occupied until its lease expires, so takeover waits on the TTL rather than the next tick. **Rejected:** claim-per-tick — it couples exclusivity to pacing and re-runs arbitration on every interval.
