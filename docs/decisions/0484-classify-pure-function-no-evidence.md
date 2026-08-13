---
status: accepted
date: 2026-08-13
phase: "14a"
---

# 0484 — Alert transitions are decided by one pure function, and evidence never enters it

**Context.** Every check run produces a finding that must be compared against the alert's current state to decide what, if anything, to publish. Findings also carry evidence — measurements, offending ids — which is tempting input for that comparison.

**Decision.** One pure function, `classify(found, head, repeat, now)`, compares the fresh finding against the check's compaction head on `__system.alerts` and returns what to publish. Evidence — the alert's `Data`/`Metadata` — rides along on the published alert but never enters `classify`: only identity, status, severity, and timestamps decide the transition.

**Consequences.** The transition logic is one named, testable home with no I/O; every check reuses it. Changing what evidence a check attaches can never change when it alerts, and a fluctuating measurement cannot flap an alert whose status and severity are unchanged.
