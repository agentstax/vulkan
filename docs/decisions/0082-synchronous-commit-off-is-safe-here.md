---
status: accepted
date: 2026-06-20
phase: "3.5"
---

# 0082 — synchronous_commit=off is safe for this queue: it adds no new failure mode

**Context.** `synchronous_commit=off` skips the fsync wait at commit, risking the last few hundred milliseconds of commits on a crash. Whether that risk is acceptable depends entirely on what a lost commit means for the application.

**Decision.** `off` is judged safe here. A lost success/failure record on crash means the lease expires, the row is reclaimed, and the work reruns — exactly the at-least-once contract already accepted and covered by consumer idempotency. A lost claim leaves the row `ready`, to be claimed again. No new failure mode appears; the throughput win (up to 6× at one worker on this box) is bought with risk already priced in.

**Consequences.** More duplicate work on crash, never loss or corruption — `off` is not `fsync=off`, which risks actual corruption. The deciding question generalizes: is there a recovery path for a lost commit? A queue can replay (the message is still there, plus idempotency); a system with no replay path — a ledger that told the customer "done" and then lost the commit — must keep `on`.
