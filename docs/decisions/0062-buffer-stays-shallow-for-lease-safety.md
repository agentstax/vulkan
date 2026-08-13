---
status: accepted
date: 2026-06-20
phase: "3"
---

# 0062 — The in-memory buffer stays shallow: depth is a lease-safety constraint, not a throughput lever

**Context.** Buffer depth serves two different constraints. Every buffered row already carries a live lease: `lease_until` is stamped and `attempts` incremented at claim time. For throughput, the buffer only needs to be deep enough to mask one claim's round-trip.

**Decision.** Keep the PressureQueue shallow — sized just to hide claim-SQL latency and keep workers fed, no deeper.

**Consequences.** A row that dwells in a deep buffer past its lease window gets reclaimed by another worker and double-processed, and idle buffered rows burn `attempts` toward `dead`. The shallow rule is therefore a durability constraint independent of the throughput model; raising depth for throughput must respect the lease window. **Rejected:** deep buffering as used for ephemeral in-memory work — with no lease and no durability, losing or redoing work is free; here it is not.
