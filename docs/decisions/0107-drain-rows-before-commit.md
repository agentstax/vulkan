---
status: accepted
date: 2026-06-23
phase: "4"
---

# 0107 — Claimed rows are drained with `CollectRows` before the claim transaction commits

**Context.** `ClaimMessagesV2` reads the claimed range inside the same transaction that locks and advances the cursor. A pgx connection cannot commit while a result set is still streaming — attempting it fails with "conn busy".

**Decision.** Fully drain the range read into memory with `CollectRows` before committing the claim transaction.

**Consequences.** The whole claimed batch is materialized in memory before processing starts, bounding batch size by memory rather than allowing streaming consumption. Any future claim query in this codebase inherits the same rule: collect inside the transaction, commit, then process.
