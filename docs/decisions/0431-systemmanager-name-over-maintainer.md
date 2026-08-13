---
status: accepted
date: 2026-08-09
phase: "14a"
---

# 0431 — The umbrella package is systemmanager, not Maintainer

**Context.** The top-level package that claims the system manager row and runs everything in the deployment needed a name; Maintainer was the incumbent candidate from the old machinery.

**Decision.** `systemmanager`, after researching how comparable systems name the umbrella process (kube-controller-manager, the autovacuum launcher, River's QueueMaintainer): the umbrella is named for what it does to its children, and this codebase's noun for that is `manager`.

**Consequences.** The daemon command is `vulkan manager run`, matching the package. **Rejected:** Maintainer — it named the old duty machinery, not the spawn-and-reconcile relationship the process actually has to its workers.
