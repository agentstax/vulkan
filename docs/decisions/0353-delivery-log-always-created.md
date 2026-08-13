---
status: accepted
date: 2026-07-20
phase: "11.5"
---

# 0353 — delivery_log_<id> is always created; DisableDeliveryLog gates only the writes

**Context.** Previously the `delivery_log_<id>` table's very existence encoded `DisableDeliveryLog`: a topic with the flag set simply had no audit table. That gave the schema two legal shapes per version and made the flag structural rather than a plain setting. Dated approximately; built across July 2026.

**Decision.** The table is always created for every topic; `DisableDeliveryLog` gates only whether attempts are written to it.

**Consequences.** Cost: a few catalog rows plus an empty index per topic that never writes. Payoff: every topic shares one schema shape, so the flag becomes a plain alterable row field, and the migration invariant labs have exactly one legal shape per schema version instead of two. This replaces the earlier existence-encoding design.
