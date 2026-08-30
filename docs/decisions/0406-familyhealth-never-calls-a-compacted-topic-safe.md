---
status: accepted
date: 2026-08-06
phase: "14a"
---

# 0406 — FamilyHealth never reports a compacted topic safe to retire

**Context.** `MessageAdmin.FamilyHealth` reports, per registered version, the `Compacted` flag, per-group `GroupLag` (via `consumer/metrics.ConsumerGroupSnapshot.GroupLag()`), and a `Safe`/`Reason` retire verdict, to telegraph when an old version can be destroyed.

**Decision.** A compacted topic's verdict is always `Safe: false` with reason "compacted: requires bridge" — even once a bridge has actually finished migrating it. The library cannot know from lag alone that latest-per-key state has been fully carried over, so it refuses to guess rather than lie that retirement is safe.

**Consequences.** Retiring a compacted version is always a human call backed by the user's own completeness checks; schemaevolutionlab demonstrates exactly this — its own checks prove the migration complete while `FamilyHealth` keeps refusing. Non-compacted versions get a real verdict from lag.

Amended by [0618]: with the version on the row, the retire verdict for a compacted version is a query (heads still at the old version, unconsumed old-version rows per group), so the permanent refusal ends.
