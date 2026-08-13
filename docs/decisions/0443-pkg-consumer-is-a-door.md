---
status: accepted
date: 2026-08-04
phase: "14a"
---

# 0443 — pkg/consumer is a door, not a vocabulary layer

**Context.** Converting pkg/consumer (4362 lines across 17 files) to the three-layer pattern collided with one non-negotiable symbol: `consumer.NewConsumer` keeps its name and package, because it is the thing users type.

**Decision.** pkg/consumer is a DOOR rather than a vocabulary layer. The sorting rule that placed every symbol: nothing a user types may live below the door — the door imports the row packages, and the rows may not import back. Because there is no vocabulary layer, the read-models live in the controller.

**Consequences.** The package tree was derived mechanically from that one constraint plus the import arrows, not designed fresh. The consumer domain permanently deviates from the standard shape (no `pkg/<x>` vocabulary layer), a cost accepted to keep the public entry point stable.
