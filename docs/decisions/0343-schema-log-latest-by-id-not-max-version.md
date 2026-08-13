---
status: accepted
date: 2026-07-20
phase: "11.5"
---

# 0343 — One append-only schema_log table; current version is the latest-by-id success row, not MAX(schema_version)

**Context.** Both system-scope and topic-scope migrations need a durable version record, and an intentional downgrade (release rollback) inserts a row with a lower `schema_version` than the current one. Dated approximately; built across July 2026.

**Decision.** A single append-only `schema_log` table (`entity_type, entity_id, schema_version, status, error, occurred_at`) serves both scopes. The current version is the latest-by-`id` row with `status='success'`. Failure rows are pure diagnostic history, never read to decide control flow.

**Consequences.** Latest-by-id follows insertion order regardless of the version value, so a rollback row becomes current and history reads truthfully ("went to v4, rolled back to v3"). **Rejected:** `MAX(schema_version)` — a downgrade's lower version would be invisible, the system would believe it is still at the higher pre-rollback version, never re-apply the up step on a later roll-forward, and every status readout would lie; MAX is only correct if versions monotonically increase, which downgrades break by design. **Rejected:** two scope-specific tables — one table serves both scopes.
