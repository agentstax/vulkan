---
status: accepted
date: 2026-06-13
phase: "1"
---

# 0001 — Claims use SELECT ... FOR UPDATE SKIP LOCKED

**Context.** Multiple workers poll the same `message_log` table concurrently.
A plain `FOR UPDATE` claim makes the second worker block on rows the first
already locked, and no locking at all lets two workers process the same row.

**Decision.** The claim query is `SELECT ... FOR UPDATE SKIP LOCKED`: rows
already locked by another transaction drop out of the result set instead of
blocking the query.

**Consequences.** Two workers running the same claim query at the same instant
get different rows — no double-processing and no head-of-line blocking.
Skipping is safe here, where it would normally be a correctness bug, because a
locked row is in process by another worker; the work is owned, not dropped.
