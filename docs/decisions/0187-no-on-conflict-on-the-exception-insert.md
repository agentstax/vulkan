---
status: accepted
date: 2026-07-03
phase: "6.5c"
---

# 0187 — No ON CONFLICT on the exception INSERT

**Context.** The plan suggested `ON CONFLICT DO UPDATE` on the `deliveries`
INSERT that records failures, on the assumption a row might already exist.

**Decision.** Plain INSERT, no conflict clause. Tracing showed a collision
cannot happen in this design: a `message_id` belongs to exactly one range
ever (`claimed` only moves forward), and because `Commit` frees the lease
before inserting (see 0182), only the still-owning worker ever reaches the
INSERT.

**Consequences.** A real primary-key violation now surfaces loudly as an
error instead of being silently absorbed by defensive SQL — if the invariant
ever breaks, it is visible.
**Rejected:** `ON CONFLICT DO UPDATE` — defensive handling for a case with no
actual trigger, which would have masked invariant violations.
