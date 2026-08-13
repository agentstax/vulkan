---
status: accepted
date: 2026-07-11
phase: "8c"
---

# 0262 — latest_key trades a second synchronous write per keyed publish for an O(1) latest-per-key read

**Context.** The correlated "is this the latest row for its key" scan is the ground-truth definition of latest but costs O(partitions since the row) to evaluate, with no early termination for a never-superseded key.

**Decision.** A `latest_key(topic_id, compaction_key, latest_id)` table is upserted synchronously in the same transaction as every keyed write: `appendMessage`'s INSERT gained `RETURNING id`, followed by `INSERT ... ON CONFLICT (topic_id, compaction_key) DO UPDATE SET latest_id = EXCLUDED.latest_id WHERE latest_key.latest_id < EXCLUDED.latest_id`. The read path in `readMessages`/`FanOut` became `m.compaction_key IS NULL OR m.id = (SELECT latest_id FROM latest_key WHERE topic_id = $N AND compaction_key = m.compaction_key)` — the old unbounded scan was deleted outright, with no fallback path.

**Consequences.** The latest-per-key read is O(1) regardless of partition count. Every keyed publish pays a second write in its transaction — measured as noise when uncontended, and a real serialization cost on a single hot key (recorded separately). Unkeyed traffic short-circuits on `compaction_key IS NULL` and pays nothing.
