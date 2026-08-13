---
status: rejected
date: 2026-07-11
phase: "8c"
---

# 0270 — A latest_key backfill was built, verified live, then reverted

**Context.** Introducing `latest_key` raised the question of compacted-topic history written before the table existed: without a backfill, the read path would need a fallback to the correlated scan or a hard requirement to migrate.

**Decision.** A per-topic `BackfillLatestKeys` (`INSERT INTO latest_key ... SELECT ..., MAX(id) ... GROUP BY compaction_key ON CONFLICT DO NOTHING`) was implemented and proven correct live — reconstructs history, idempotent, never clobbers a live write mid-race — and then reverted outright. This project has no live deployment with compacted-topic history predating `latest_key`, so a migration mechanism for one was designing for a user that does not exist.

**Consequences.** `latest_key` is authoritative from the moment the write path lands; the read path has no fallback and the old scan was deleted outright — the fallback-versus-require-backfill sub-decision became moot. If a real pre-`latest_key` deployment ever appears, the verified backfill shape is on record to rebuild.
