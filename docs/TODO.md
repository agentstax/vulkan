# TODO

Sliding window of in-flight work only. Future work lives in ROADMAP.md;
shipped work in HISTORY.md; decision rationale in DECISIONS.md ->
docs/decisions/.

## `{schema}`-qualify every SQL literal

Production SQL joins the diagnose queries and the doc pages in writing
`{schema}.table`, filled once at query build from the datastore's own
`Schema`. search_path keeps its one remaining job: resolving the
caller's own statements inside the produce transaction.

Why: search_path is `"<schema>, public"`, and public is load-bearing
for the InTransaction seam, so any table missing from the installation's
schema but present in public resolves silently to public's copy. Two
installations in one database is exactly what [0628] blessed, and topic
ids are per-installation serials, so the collisions are real:

- Wrong-table reads: every 42P01-means-absence mapping (implicit
  RegisterSystem, catalog reads -> VK0005) finds the public
  installation's catalog instead of seeing absence.
- Destructive: a destroy retry where the schema's `message_log_1` is
  already gone resolves `DROP TABLE IF EXISTS message_log_1` through
  search_path and drops the public installation's topic.

### Tasks

1. ~~**The fill.**~~ DONE. No helper: literals interpolate
   `d.Datastore.Schema` with plain `fmt.Sprintf`, standard and
   vet-checked. The house rule is **schema is always `%[1]s`**, tables
   follow as `%[2]s`, `%[3]s` -- the schema repeats at every table
   reference (23 of them in `deliveryconsumer.fanOut`), so indexed verbs
   are the only shape that does not pass it N times.
   `topic.MessageLogTable` and its siblings stay name-only; the literal
   writes `%[1]s.` ahead of the interpolated name.
2. ~~**The owner-commented literals.**~~ DONE. 245 table references
   qualified across 53 files; 20 literals correctly untouched (the six
   advisory locks, three `SET LOCAL lock_timeout`, `SELECT now()`,
   `CREATE SCHEMA`, and the pg_catalog reads). 27 literals that sat
   inline in the `QueryRow(ctx, ...)` call became assigned locals, and
   one built by `+` concatenation became a `Sprintf`. Five literals live
   in free funcs with no receiver (`migrate.Version`,
   `migrate.SystemOwner`, `producer.protectedInsertSQL`,
   `messageconsumer.deliveryStatement`/`logStatement`) and took a
   trailing `schema string` param.
3. **DDL — 38 CREATE statements in 6 files** (system tables, topic
   tables, partitions, migrate). Qualify the created table and the
   `PARTITION OF` parent; `CREATE INDEX` qualifies only its `ON` clause
   (index names cannot carry a schema and land in the table's schema);
   `createSchema`'s `CREATE SCHEMA %s` is already explicit and stays.
4. **The nine name-as-value sites** — the class a literal walk cannot
   see, swept by hand: 4x `to_regclass($1)` (drain x2, both partition
   alerts), `'%s'::regclass` (janitor partition list),
   `SELECT last_value FROM %s` (heal's sequence read), and 4x
   `DROP TABLE IF EXISTS %s` (topic delete, drain, janitor drop, system
   delete). The schema is prepended to the value passed in. This task
   is the bug fix; the rest is what keeps it fixed.
5. **tools/conventions.** Extend the diagnose-query table walk to
   production literals — an unqualified table reference is a test
   failure, so a missed `{schema}.` is loud instead of silently rescued
   by search_path. `table_ddl_test.go` strips the `{schema}.` prefix
   before its `<root>_<kind>` checks. Sabotage-verify both.
6. **Sandbox mirror re-sync.** sql.test.ts compares ~30 mirrored
   statements byte-exact against the Go source, so every mirrored
   literal carries the same `{schema}.` bytes; `interpolate.ts` gains a
   `{schema}` replacement so PGlite fills a stand-in (the
   sandboxGroupLockKey pattern).
7. **Decision record** superseding the no-qualified-SQL half of [0628],
   linked both ways.
8. **The labs.** 242 unqualified table references across 45 files under
   examples/. They work today -- a lab connects through the datastore, so
   search_path resolves them -- but they are the "user writing a
   diagnostic query" case [0628] names, and they hand-write
   `message_log_%d` instead of calling `topic.MessageLogTable`, which
   that record already forbids. Scope call at pickup: qualify them, route
   them through the table-name funcs, both, or neither.
9. **Verification.** `just verify`, directly-affected labs per change;
   full fresh-DB suite + `just compat-lab` + `npm run verify` at the
   review-ready checkpoint. The pinned compat build is unaffected — its
   own SQL is unqualified and resolves through its search_path as
   before.

Open at pickup: whether search_path slims from `"<schema>, public"` once
vulkan's own SQL no longer needs the leading entry — the schema entry
becomes belt-and-braces, but keeping it is the smaller delta and CREATE
placement for any future unqualified statement still favors it.
