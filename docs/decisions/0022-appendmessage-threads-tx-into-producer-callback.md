---
status: accepted
date: 2026-06-14
phase: "1.5"
---

# 0022 — AppendMessage threads its transaction into a producer callback

**Context.** For the enqueue and the business write to be atomic, the caller's
business statements must run on the same transaction that inserts the message
— the API has to expose that transaction without giving up control of commit
and rollback.

**Decision.** `AppendMessage` opens the transaction, calls the caller's
`ProducerFunc(ctx, tx)` so the business write runs on that same transaction,
then `INSERT`s into `message_log` and commits. Any error on either path trips
the deferred `Rollback` and unwinds both writes. This is the insert-only
client shape used by River.

**Consequences.** There is no window where the business row exists without the
message or vice versa. Commit and rollback stay owned by `AppendMessage`; the
callback only contributes statements. The producer package's API is
transaction-shaped, which is what couples it to a concrete driver.
