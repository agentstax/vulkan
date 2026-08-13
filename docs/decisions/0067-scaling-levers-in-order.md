---
status: accepted
date: 2026-06-20
phase: "3"
---

# 0067 — Scaling levers are applied in a fixed order

**Context.** With the ceiling model settled as `min(supply, ack_capacity)`, several levers could each raise throughput; applying them out of order wastes effort on a bound that is not currently binding.

**Decision.** The order is: keep claims cheap (index) → raise batch and buffer to clear supply → add workers for commit capacity → batch the per-message commits → multiple prefetchers (only needed past ~290k/s supply) → archive terminal `done`/`dead` rows.

**Consequences.** Each lever targets the bound the previous one exposes, so measurements stay attributable to one change. Multiple prefetchers are explicitly deferred as unnecessary at current scale; archiving terminal rows is deferred to later retention work.
