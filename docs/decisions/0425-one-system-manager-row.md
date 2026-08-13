---
status: accepted
date: 2026-08-09
phase: "14a"
---

# 0425 — One system manager row; the daemon is the same manager at deployment scope

**Context.** With consumption loops as worker rows, the daemon (`vulkan manager run`) needed a claim anchor and a deployment-wide scope. Per-topic manager rows existed from earlier work.

**Decision.** pkg/systemmanager is the top-level door: the daemon is the SAME manager, claiming the single system manager row instead of a group's rows. "Run everything in the deployment" is one extra WHERE clause in listWorkers — only a system owner has both topic and group unset. Topic manager rows were deleted.

**Consequences.** N daemons arbitrate over the system manager row with the same `worker_instance` machinery as everything else, and the row doubles as the deployment-wide suspend switch. **Rejected:** per-topic manager rows — nothing runs at topic scope, so their rows were deleted rather than kept as a third tier.
