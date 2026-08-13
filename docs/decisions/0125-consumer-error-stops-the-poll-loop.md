---
status: accepted
date: 2026-06-23
phase: "5"
---

# 0125 — A `consumerFunc` error stops the whole poll loop; per-group retry and DLQ are deferred

**Context.** The cursor model has no per-row failure resolution — a single integer cannot record that one message failed while its neighbours succeeded. Fan-out adds nothing to change that.

**Decision.** Keep failure semantics unchanged: a `consumerFunc` error returns up through `Claim` and `Process` and stops the poll loop. The fan-out lab runs at `fail-rate=0` to keep the focus on independent positions.

**Consequences.** A failing message head-of-line blocks its group until the operator intervenes. This is a deliberate deferral, not a gap: per-group retry and dead-lettering are the later `deliveries` work. Trying to solve failure inside the bare cursor model would reintroduce per-row state the split just removed.
