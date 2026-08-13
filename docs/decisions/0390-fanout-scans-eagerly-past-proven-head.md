---
status: accepted
date: 2026-08-01
phase: "14"
---

# 0390 — FanOut delivers eagerly past the proven head; only the mark waits for proof

**Context.** The cursor-claim path must stop at a proven head because claims seal ranges permanently. FanOut faced the same straggler hazard — a visible row above a still-open lower transaction — but its write path has different idempotency properties.

**Decision.** FanOut's scan runs eagerly past the proven head: a visible row delivers this tick even while a straggler transaction below it is open, because the delivery primary key plus `ON CONFLICT DO NOTHING` makes next tick's rescan of the same range a no-op. Only the high-water mark requires proof before advancing, since territory below the mark is never rescanned.

**Consequences.** Fresh messages reach LIFECYCLE consumers without waiting out overlapping producer transactions — the mark may lag freely behind delivery. The invariant: re-scanning un-hardened territory must stay idempotent, which the delivery PK guarantees; any future change to delivery materialization must preserve that or move to the claim gate's stop-at-proven-head rule.
