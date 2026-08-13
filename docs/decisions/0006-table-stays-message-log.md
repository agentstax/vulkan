---
status: accepted
date: 2026-06-13
phase: "1"
---

# 0006 — The table is named message_log, not jobs

**Context.** The build plan's text calls the queue table `jobs`, but the code
already uses `message_log`, and a deliberate log/queue split is planned that
will be the natural moment to settle names.

**Decision.** Keep `message_log` and defer the rename to the log/queue split
rather than renaming now.

**Consequences.** Naming diverges from the plan text in the interim; the table
gets renamed at most once, at the moment its role is actually redefined,
instead of twice.
