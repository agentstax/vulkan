---
status: accepted
date: 2026-09-01
phase: "pre-v1"
---

# 0632 — The pool sets no search_path

**Context.** [0631] qualified every SQL literal but left `search_path` at `"<schema>, public"`, keeping the leading entry as belt-and-braces and naming the one thing it could not cover: `migrate.Migration`'s step funcs take `(ctx, q, topicId)` and reach no datastore, so a post-v1 step could not name its schema and would have resolved through the path. Meanwhile the entry carries a cost [0630] recorded and accepted: `InTransaction` runs the caller's own SQL on Vulkan's pool, so `CREATE TABLE orders` inside that closure lands in Vulkan's schema rather than the caller's.

**Decision.** A step's funcs take the schema beside the topic id — `func(ctx, q, schema string, topicId int64) error` — and the migrate datastore passes `d.Datastore.Schema` at each of the four call sites. Free now: both registries are empty, and once a step ships it is immutable. With that closed, `NewPostgresDatastore` sets no `search_path` at all; the connection keeps its own default. Every Vulkan statement names its schema, so nothing of Vulkan's depends on the path, and the caller's statements inside `InTransaction` resolve the way they would on any other connection.

**Consequences.** [0631]'s clause giving `search_path` the InTransaction job is superseded; the rest of it stands. The wart [0630] accepted is gone, and the schema lab asserts it: a caller's `CREATE TABLE` inside `InTransaction` lands in `public`, and restoring the path makes that assertion fail with the table in Vulkan's schema. Absence semantics are unchanged — a *read* through a missing schema is still `42P01`, which the catalog reads already map, and only a *write* raises `3F000`, which `CREATE SCHEMA IF NOT EXISTS` precedes. `tools/compat`'s guard read `migration_log` unqualified and now names `ds.Schema`; the pin flow loses its `--schema public` step for any two builds that both name their own schema, keeping it only for a build old enough to predate the field. The labs that assemble a table name from a variable root, and `destroysystemlab`'s control-plane list, had to be qualified — the path had been resolving them. **Rejected:** setting `search_path` to `public` alone, which is the connection default in every deployment this targets and states as configuration something Postgres already does.
