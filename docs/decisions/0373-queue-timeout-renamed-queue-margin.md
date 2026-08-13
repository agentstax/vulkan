---
status: accepted
date: 2026-07-28
phase: "13"
---

# 0373 — QueueTimeout is renamed QueueMargin; WorkTimeout and AckMargin keep their names

**Context.** `WorkTimeout`, `QueueTimeout`, and `AckMargin` all sum into `leaseDuration`, and all three carried "consider a better name" TODOs. Checking actual enforcement, not just the formula, split them into two real categories.

**Decision.** `WorkTimeout` and `AckMargin` are each independently enforced elsewhere — `WorkTimeout` bounds `consumerFunc`'s own ctx plus the hard-abandon fallback; `AckMargin` bounds the `Commit`/`PartialCommit` call itself — so their names already match their roles and stay. `QueueTimeout` was the odd one out: never independently enforced anywhere, pure additive lease padding, structurally identical to `AckMargin`'s role — renamed `QueueMargin` to match.

**Consequences.** The naming TODOs on all three are removed. `QueueMargin`'s existence (not just its name) hung on the queue/pool-limiter decision; the later buffered-claim-plus-dispatch design kept it with real meaning — time buffered before a processor starts eats the lease, and the bounded buffer is what bounds it.
