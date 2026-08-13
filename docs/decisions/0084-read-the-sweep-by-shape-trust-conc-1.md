---
status: accepted
date: 2026-06-20
phase: "3.5"
---

# 0084 — The on/off sweep is read by shape with best-of-3/max, and conc=1 is the number to trust

**Context.** The `synchronous_commit` on/off sweep (batch=100, 20000 messages, fresh-seeded backlog, Docker postgres:18) shows large run-to-run variance at high concurrency (on@8: 2280–6586 msgs/s; off@64: 14739–27357).

**Decision.** Estimate each cell as best-of-3 maximum, because benchmark noise is one-directional — interference only slows a run. Treat the robust finding as the shape, not the peaks: the off/on gap is largest at one worker (5.99×) and shrinks with concurrency (1.28× at 64), because group commit already merges concurrent commits into one physical fsync under `on`. Trust the conc=1 row as the clean signal: one commit in flight, fsync fully exposed versus fully skipped — 581µs versus 97µs per record, so ~484µs is the fsync wait on this storage.

**Consequences.** The 6× ratio is this box, not a constant: absolute fsync cost is set by the Docker Desktop VM volume, so faster durable storage compresses the ratio and slower disk widens it. The `on` baseline at 64 workers (21.4k/s) reproduces the previously measured ceiling, validating the harness. **Rejected:** reading peak high-concurrency numbers as the finding — variance there is too large to support more than the shape.
