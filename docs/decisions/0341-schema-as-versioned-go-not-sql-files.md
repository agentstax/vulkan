---
status: accepted
date: 2026-07-20
phase: "11.5"
---

# 0341 — Schema lives as versioned Go code; golang-migrate and its .sql files are deleted

**Context.** Schema bootstrap and evolution had lived in golang-migrate `.sql` files applied by justfile recipes, outside the library's own API — nothing in code could register a topic's tables or advance a schema version. Dated approximately; built across July 2026.

**Decision.** `pkg/migrate` runs schema changes as versioned Go migrations through a runner: `RunOnce` migrates one entity, `RunAll` loops every topic and continues past a per-topic failure, joining errors with `errors.Join`. `migrations/*.sql`, `migrations/old/`, and the justfile `migrate-up`/`migrate-down` recipes were removed; `RegisterSystem`'s idempotent CREATE-IF-NOT-EXISTS baseline is the bootstrap path.

**Consequences.** Schema is applied through the same guarded API surface as everything else, per-topic table families can be migrated per topic, and a partial multi-topic failure reports every topic's error instead of stopping at the first. There is no `.sql` file trail to keep in sync with code.
