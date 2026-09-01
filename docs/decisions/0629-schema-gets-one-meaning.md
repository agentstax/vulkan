---
status: accepted
date: 2026-09-01
phase: "pre-v1"
---

# 0629 — "schema" names a Postgres schema and nothing else

**Context.** Found while planning [0630], not planned work. The word already carried two senses — a payload's version (`SchemaVersion`, `message_log.schema_version`, [0618]) and a migration target (`vulkan migrate`'s rows, `pkg/migrate`'s prose, ~25 comments calling the shared tables "the control-plane schema") — and [0630] adds a third, the Postgres namespace. Postgres owns that word, and ## Vocabulary bans coining an alternative for a mechanism's own noun, so the migration sense is the one that moves. The CLI was already inconsistent with itself about it: `migrate up/down` emitted `{"scope": ...}` while `migrate status` emitted `{"schemas": [{"schema": ...}]}` for the identical concept.

**Decision.** No rename — remove the need for a collective noun. `migrate status --output json` reports `system` as an object beside a `topics` array, and the text output prints one `system:` line above a `TOPIC` table. The result documents drop `scope` and `schema` entirely: the command already names what it targeted, so the document reports only what happened (`topic` present when one topic was named, absent otherwise). VK0017's problem line becomes "system not registered". The ~25 "control-plane schema" sites read "the control-plane tables". Two ## Vocabulary rows record it, and `schema` joins the ### Attributes registry as the Postgres namespace.

**Consequences.** This fixes a real ambiguity, not just a word: only the `__system.` PREFIX is reserved, so a topic named exactly `system` registers, and `migrate status` printed two indistinguishable rows for it in both output modes. Splitting the list by what the rows are makes it unambiguous by construction, and the system needs no name because a database holds exactly one — the singleton [0625] settled and [0630] makes permanent. `--output json` is a contract [0575][0576]; pre-v1, so the break is taken, and nothing in the labs, the site, or tools consumed the old keys. **Rejected:** `common.Owner`/`owner_kind` as the collective noun — it is the library's right name for a polymorphic row's owner but reads backwards at a surface where nothing is being owned. **Deferred:** "schema version" for a MIGRATION version (VK0022/VK0023's problem lines, `ErrSchemaOlderThanBuild`/`ErrSchemaNewerThanBuild`, ~8 files). The rule if it is taken up: `migration_log.migration_version` reads "migration version" and `message_log.schema_version` keeps "schema version", so guides/schema-versions.mdx never moves.
