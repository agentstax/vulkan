---
status: accepted
date: 2026-08-06
phase: "14a"
---

# 0403 — Compaction's winner rule generalized to a signed caller-supplied rank compared before id

**Context.** Compaction's rule was "arrival order wins." A bridge consumer re-producing old data into a live topic races that topic's own producers: whichever write lands last wins, so a stale backfill landing after a fresh live write would silently overwrite it.

**Decision.** `CompactionRank BIGINT NOT NULL DEFAULT 0` on `message_log` and `compaction_head` (the winner's rank is stored, not just its id), set via `ProduceOptions.CompactionRank`. Both upsert guards — `pkg/producer/datastore.go`'s `protectedInsertSQL` and `batch.go`'s batch path — became the native row compare `(compaction_head.compaction_rank, compaction_head.head_id) < (EXCLUDED..., EXCLUDED...)`. The rank is signed so the bridge can write below the default: bridge writes at rank −1, live producers at their default rank 0, and a live write always wins regardless of which one the database committed first.

**Consequences.** The migration race becomes a declarative comparison instead of a timing outcome. All-default traffic reproduces the old semantics bit-for-bit, and every losing row stays physically present, just never claimed. `examples/phase_1/compactionranklab` proves the pin, the two-arrival-order bridge race, and the losing-row retention.
