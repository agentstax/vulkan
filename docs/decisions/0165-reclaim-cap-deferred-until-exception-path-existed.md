---
status: accepted
date: 2026-06-30
phase: "6.5b"
---

# 0165 — The cap on repeated reclaims was deferred until the exception path existed

**Context.** A range whose processing crashes the worker expires its lease
and gets reclaimed, crashes again, and is reclaimed forever — the lease
mechanism alone has no exit for a poison range.

**Decision.** Ship lease-based crash recovery without a reclaim cap. There
was nowhere to put the offending offsets yet: per-message failure rows in
`deliveries` did not exist on the cursor path. The cap and the `reclaims`
counter were deliberately scheduled for the same change that introduced the
exception path, as a named handoff rather than a silent hole.

**Consequences.** In the interim a poison range loops through reclaim
indefinitely and pins `committed` at its `low`. The handoff was honored: the
follow-up added `lease.reclaims` and `MaxRangeReclaims`, moving a
repeatedly-reclaimed range's messages into per-message `deliveries` rows
(see 0191).
