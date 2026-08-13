---
status: accepted
date: 2026-06-13
phase: "1"
---

# 0002 — The message DELETE commits in the same transaction as the claim

**Context.** A delete separated from the claim is unsafe in both orderings:
delete after processing in its own transaction can fail post-work, leaving the
row to be claimed and processed again; delete before processing loses the work
forever if the worker crashes mid-process.

**Decision.** One transaction per message: claim (`SELECT ... FOR UPDATE SKIP
LOCKED`) → process → `DELETE` the row from `message_log` → `COMMIT`.

**Consequences.** Either the commit lands (processed and deleted) or nothing
happened and the row is still claimable — atomicity is the entire durability
story. A `kill -9` mid-process needs zero recovery code: the dropped connection
rolls the transaction back, Postgres releases the lock, and the row is
claimable on the next poll.
**Rejected:** delete in a separate transaction, either ordering — duplicate
work or lost work.
