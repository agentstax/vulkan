---
status: accepted
date: 2026-07-03
phase: "7"
---

# 0207 — binding.kind, header_match, and their CHECK constraint were dropped

**Context.** An earlier draft of the `binding` table carried a `kind`
discriminator column, a `header_match` column, and a `CHECK` constraint tying
them together, anticipating multiple matcher styles.

**Decision.** All three were dropped. With only one matcher style shipped
(see 0206), the table is `binding(consumer_group, pattern, display)` — there
is nothing to discriminate between.

**Consequences.** No speculative schema: if a second matcher style ever
lands, the discriminator gets added when there are actually two things to
tell apart, shaped by the real second case rather than a guess.
