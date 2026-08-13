---
status: accepted
date: 2026-08-12
phase: "14a"
---

# 0469 — Vendor the robfig cron schedule core instead of hand-rolling or adding a dependency

**Context.** The scheduler needs cron-expression parsing and next-time computation. Main-module dependencies are held to the minimal set, and battle-tested code for this exists.

**Decision.** The robfig cron schedule core was vendored — source plus tests plus license, with provenance headers — with a single local diff: `time.Local` changed to UTC. Schedule validation on top of it enforces a minimum rate of 1 minute and a rate no shorter than the job's timeout.

**Consequences.** No new module dependency; the parser is proven code with its own tests running in-repo, and the one local diff is marked and auditable. The rate floor keeps schedules compatible with the scheduler's 1-minute poll, and the rate >= timeout rule keeps a run from outliving the gap to its successor. **Rejected:** hand-rolling cron parsing — nothing about this problem is house-specific.
