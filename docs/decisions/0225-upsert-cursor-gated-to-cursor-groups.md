---
status: accepted
date: 2026-07-08
phase: "8a"
---

# 0225 — Register creates cursor rows only for CURSOR-type groups

**Context.** `Register` called `UpsertCursor` unconditionally for every
group. A LIFECYCLE group's cursor row never advances `committed` — that is
CURSOR-only behavior — so it would sit at 0 forever. With the retention drop
floor computed as `MIN(committed)` across `cursor`, that one row permanently
pinned the floor and blocked every drop.

**Decision.** `UpsertCursor` in `Register` is gated to CURSOR-type groups
only; LIFECYCLE groups get no cursor row.

**Consequences.** Only groups that actually advance `committed` participate
in the drop-floor computation. Invariant: a `cursor` row implies a
CURSOR-type group — any mechanism aggregating over `cursor` can assume the
row's `committed` is live, not a stuck zero.
