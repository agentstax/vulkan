---
status: accepted
date: 2026-07-03
phase: "7"
---

# 0202 — Routing is one shared predicate evaluated at read time in both consume paths

**Context.** Two consume models exist — the CURSOR path's range claims and
the LIFECYCLE path's `FanOut` materialization — and both had to honor
bindings without a matching engine per model or a routing decision baked in
at produce time.

**Decision.** The identical `NOT EXISTS (binding) OR EXISTS (matching
binding)` predicate is pushed into both existing reads. CURSOR:
`readMessages` gains a `consumerGroup` param and the predicate in its `WHERE`
— a non-match is excluded from what is returned, but the cursor still
advances over the whole claimed range, so `committed` stays a dense frontier
and a non-match counts as resolved with no work, with no `deliveries` row
recorded. LIFECYCLE: `FanOut`'s `SELECT` gains the same predicate, so a
non-match never materializes a `deliveries` row at all. Evaluation happens at
claim/fan-out time against the `binding` rows present then, never at produce
time.

**Consequences.** No new plumbing — routing is a `WHERE` clause on two reads
that already existed, and both models absorb one shared `binding.pattern`
string. A binding added after a message was published still applies to it if
the message is not yet claimed (CURSOR) or fanned out (LIFECYCLE) — verified
live in `routinglab` — and has zero effect on anything already resolved.
