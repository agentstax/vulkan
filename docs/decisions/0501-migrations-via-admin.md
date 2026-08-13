---
status: accepted
date: 2026-08-01
phase: "13"
---

# 0501 — Schema provisioning is a library call, not an external migrate step

**Context.** The project had to decide whether users run schema migrations through an external `migrate` step or through functions they call directly — a shape decision on the public surface, not an implementation detail.

**Decision.** Users migrate via `admin.MigrateTopic`/`admin.MigrateTopics`/`admin.MigrateSystem` on `admin.MessageAdmin`. The whole `migrate` package plus `topic/migrations.Registry` and `system/migrations.Registry` go internal; the CLI keeps access through `internal/` — Go's import-path prefix rule spans the nested module.

**Consequences.** One provisioning door for users, with the admin object as its home; migration registries stop being public API anyone can drive out of order. The CLI's access rides on module layout, not on re-exporting the machinery.
