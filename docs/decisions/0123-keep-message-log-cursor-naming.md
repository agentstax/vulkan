---
status: accepted
date: 2026-06-23
phase: "5"
---

# 0123 — The codebase keeps `message_log`/`cursor`/`position` naming rather than renaming to events/consumers

**Context.** The fan-out design was planned in terms of "events" and "consumers", while the code already uses `message_log`, `cursor`, and `position` from the log/queue split. The shapes are the same; only the names differ.

**Decision.** Keep the existing names. No rename churn to match the planning vocabulary.

**Consequences.** A reader of older planning material must map "events" to `message_log` and a consumer's offset to `cursor.position`. In exchange, the schema, code, and labs stay internally consistent, and the codebase's own nouns remain the single naming authority.
