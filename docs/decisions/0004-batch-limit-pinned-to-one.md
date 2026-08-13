---
status: accepted
date: 2026-06-13
phase: "1"
---

# 0004 — Batch limit pinned to 1

**Context.** Claiming several messages in one transaction ties their fates
together: one poison message that fails or hangs poisons the whole batch,
holding or rolling back messages that would have succeeded.

**Decision.** The claim batch limit is pinned to 1 — each transaction claims
and processes a single message.

**Consequences.** Batch poisoning is impossible by construction. The cost is
one claim round-trip per message, an accepted throughput ceiling at this
scale.
