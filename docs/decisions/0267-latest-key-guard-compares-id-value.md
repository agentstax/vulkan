---
status: accepted
date: 2026-07-11
phase: "8c"
---

# 0267 — The latest_key upsert guard compares id values, not commit order

**Context.** `BIGSERIAL` allocates ids at INSERT time, not commit time, so two concurrent same-key publishes can commit in the opposite order of their ids under READ COMMITTED. A plain last-commit-wins upsert would let the row holding the older id overwrite the newer one.

**Decision.** The keyed publish's upsert carries an explicit value guard: `ON CONFLICT (topic_id, compaction_key) DO UPDATE SET latest_id = EXCLUDED.latest_id WHERE latest_key.latest_id < EXCLUDED.latest_id`. An update only lands if it raises `latest_id`.

**Consequences.** `latest_key` converges to the highest id for a key under real concurrent same-key races regardless of commit interleaving (proven live by `latestkeysracelab`). **Rejected:** unconditioned `DO UPDATE` — silently wrong exactly when concurrent publishes commit out of id order.
