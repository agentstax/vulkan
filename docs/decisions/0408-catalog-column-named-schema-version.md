---
status: accepted
date: 2026-08-06
phase: "14a"
---

# 0408 — The topic catalog column is named schema_version, accepting the overlap with schema_log.schema_version

**Context.** The topic catalog's new version column shares a name with `schema_log.schema_version`, which is the DB-migration axis owned by `pkg/migrate`. The topic column is a different axis entirely: the message compatibility epoch a user sets.

**Decision.** Keep the name `schema_version` for both. The two columns are never read in the same query and are documented at both definitions.

**Consequences.** **Rejected:** disambiguating the topic column (e.g. `topic_version`) — judged not worth losing the more natural word for what a user actually sets via `--schema-version`. Readers encountering either column should check which axis they're on; the definitions carry that note.
