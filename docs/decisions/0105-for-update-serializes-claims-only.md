---
status: accepted
date: 2026-06-23
phase: "4"
---

# 0105 — `FOR UPDATE` on the cursor serializes concurrent claims only, not the process-then-advance window

**Context.** `ClaimMessagesV2` runs one transaction: `SELECT position … FOR UPDATE` (erroring via `pgx.ErrNoRows` for an unregistered group), then the ordered range read. The transaction commits before any message is processed, so the lock never covers processing or the cursor advance.

**Decision.** Accept that scope: the row lock exists to serialize concurrent claim transactions on one cursor, nothing more. No exclusion mechanism is added for the window between claim and advance.

**Consequences.** Two consumers on the same group could both process the same range — fine under the one-consumer-per-cursor model this design assumes. Real cross-consumer exclusion within a group is deliberately deferred to the later lease and exception-window work rather than bolted on here.
