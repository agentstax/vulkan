---
status: accepted
date: 2026-06-15
phase: "2"
---

# 0045 — The stuck window is the work timeout plus a 5s buffer

**Context.** Reclamation treats `locked_at` older than the stuck window as a
crashed worker. If the window is shorter than the time a live worker is
allowed to run, a still-processing worker gets reclaimed and its message is
processed twice by accident.

**Decision.** The stuck window is `workTimeout + 5s`: the buffer keeps every
worker that is still inside its legitimate work timeout safely outside the
reclamation predicate.

**Consequences.** The buffer exceeding the work timeout is an invariant —
shrinking either side of it re-opens accidental double-processing. Eventually
the buffer should become configurable rather than a constant.
