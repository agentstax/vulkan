---
status: accepted
date: 2026-08-06
phase: "14a"
---

# 0402 — Migrating a compacted topic to a new version is a user-space bridge consumer, not a library verb

**Context.** With a schema version bump being a new physical topic, a compacted source topic still holds live latest-per-key state that retention will never drain. Something has to copy that state into the new version.

**Decision.** The migration is the bridge consumer pattern: a plain user-space consumer group on the old version that re-produces each message into the new version. The library provides the primitives the bridge needs — `MessageMeta` for the source message's identity and `ProduceOptions.CompactionRank` for race-free ordering against live producers — but no built-in backfill verb.

**Consequences.** Zero-pause migration works only because the fork model (isolation) and the compaction rank (a total order that survives migration-time races) combine — neither alone gets there. The bridge is ordinary consumer code: it resumes from its persisted cursor after a crash, and its idempotency keys make re-produces no-ops. `examples/phase_1/schemaevolutionlab` is the pattern's end-to-end reference implementation.
