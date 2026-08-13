---
status: superseded
date: 2026-06-13
phase: "1"
---

# 0003 — The row lock is held for the entire processing duration

**Context.** With claim, processing, and delete inside one transaction, the
`FOR UPDATE` lock — and the DB connection carrying the transaction — stays
held for as long as the handler runs.

**Decision.** Accept that a slow handler holds its row lock and a connection
for the whole processing time. At small scale this is fine, and it is what
makes crash recovery free: rollback releases the lock.

**Consequences.** A long-running job pins a transaction and a connection for
its full duration, which does not scale under real concurrency. Superseded by
moving the claim out of the DB lock and into row data (`status='processing'`
plus `locked_at`), shrinking the lock to the millisecond claim statement.
