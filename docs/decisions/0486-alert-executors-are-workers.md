---
status: accepted
date: 2026-08-13
phase: "14a"
---

# 0486 — Alert executors are worker definitions the manager claims, never embedded goroutines

**Context.** The check executors were first hosted as errgroup goroutines inside `SystemManager.Run`. Hosted that way they were invisible to the fleet, could not be suspended, had no claim arbitration across instances, and made the system manager non-generic — it knew about alerts specifically.

**Decision.** Each executor is an ordinary worker definition that joins the manager beside janitor, cronscheduler, and waterline; the manager claims it like any other worker.

**Consequences.** Executors get fleet visibility, suspension, and N-way claim arbitration from the worker machinery, and the system manager stays generic — it hosts worker definitions, not alert code. **Rejected:** errgroup goroutines inside `SystemManager.Run` — a parallel hosting mechanism beside the worker machinery that already exists for exactly this.
