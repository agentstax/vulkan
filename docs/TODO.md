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
3. ~~**DDL.**~~ DONE. 34 statements across the two `tables.go` files:
   the created table, the `PARTITION OF` parent, and every FK
   `REFERENCES` target qualified; `CREATE INDEX` qualifies only its `ON`
   clause, index names staying bare so they land in the table's schema.
   `createSchema`'s `CREATE SCHEMA %s` and the six advisory locks stay as
   they were. Five index literals living as `[]string{...}` elements each
   became their own `fmt.Sprintf`. 298 qualified references tree-wide,
   zero unqualified.
   Also fixed here, because task 3 broke it: `table_ddl_test.go` read the
   table name straight off the CREATE line and skipped any name holding a
   `%`, so once the shared tables read `%[1]s.system_config` all 13 fell
   out of the kind check and it passed while walking nothing. It now
   trims the `%[1]s.` qualifier first. Sabotaged both ways -- a bogus
   `system_bogus` table passed before the fix and fails after.
4. ~~**The name-as-value sites.**~~ DONE. Four of the nine turned out to
   be ordinary in-literal forms task 2 had already swept -- the four
   `DROP TABLE IF EXISTS` and the sequence read -- because `DROP TABLE`
   and `FROM` are reference keywords the walk knows. The five genuinely
   value-borne ones: 4x `to_regclass($1)`, where the name rides a bind
   parameter and is now built qualified in Go, and `'%s'::regclass` in
   the janitor's partition list, qualified inside the SQL string. Its
   sibling `REPLACE(c.relname, '%[2]s_', '')` stays BARE on purpose --
   `pg_class.relname` is unqualified, so the prefix it strips must be too.
   Proved rather than asserted: schemalab section 3 now stands a whole
   installation in public, the schema every search_path ends with, and
   asserts an unregistered schema still reads absence. Sabotaging
   `topic.get` back to an unqualified `FROM topic_config` leaves both
   original assertions passing and fails the new one with
   `got schemalab.orders` -- the cross-installation read, demonstrated.
5. ~~**tools/conventions.**~~ DONE. `sql_schema_test.go` walks every
   `-- vulkan:` literal in pkg/ and cmd/ through the Go AST and checks
   each relation reference is exactly `%[1]s.<name>` — 298 references
   across 208 literals, the same counts an independent scan found. It
   splits on `.` rather than testing a prefix, so double qualification
   fails too. Sabotaged four ways, each producing its own message:
   unqualified, double-qualified, qualified with the wrong verb, and the
   `walked == 0` guard when the patterns match nothing.
   Deliberately out of its reach, and said so in the test: a name that
   reaches Postgres as a bind parameter or inside a quoted string (task
   4's class) has no keyword in front of it to match on.
   `schemaVerb` is now the one home for `%[1]s`; `table_ddl_test.go`
   builds its qualifier from it. Note `tools/` is its own module reading
   library source as data, so `go test` caches a pass straight across
   library edits — these runs need `-count=1` to mean anything.
6. ~~**Sandbox mirror re-sync.**~~ DONE. All 10 mirror cases failed
   first, which is the drift test doing its job; 45 templates re-synced
   byte-exact from the Go source, matched by position through the case
   list rather than by guessing.
   `interpolate` now reads indexed verbs and fills `%[1]s` itself with
   PGlite's own `public`, so no call site passes a schema and all 26 stay
   unchanged; values fill `[2]` onward. A template whose verbs outrun its
   values throws instead of reaching PGlite half-filled.
   Both `statements.ts` files now hold the same two lists side by side --
   `*Templates` raw for the drift test, `*Statements` filled for the
   sandbox -- because the system side's one array was serving both roles
   and its raw form is no longer runnable. The topic side needed the same
   move in the other direction: its raw list was assembled inside
   sql.test.ts, so the two orderings of one Go method sat in different
   files. sql.test.ts drops 16 imports, and a test asserts the two lists
   in each file are the same length, so a statement added to one of them
   fails rather than merely being visible.
   Four unit tests pin the fill contract; an off-by-one in the value
   index fails two of them. Playwright's sandbox-boot runs are the
   end-to-end proof: PGlite creates and queries the tables for real.
7. ~~**Decision record.**~~ DONE. [0631], superseding [0630]'s "no SQL
   changes" clause and its rejection of qualifying; the rest of [0630]
   stands and links back. The plan named [0628] here, which is wrong --
   that record is the table-name funcs going public; the clause reversed
   is [0630]'s. CONVENTIONS ## SQL carries the binding rule, since a
   record is rationale and the rule file is what governs code.
8. ~~**The labs.**~~ DONE, both halves. The scope call was already made
   by CONVENTIONS ## Tables, which names labs explicitly -- [0628] removed
   the lab exception and the labs were never updated. All 217 per-topic
   SQL sites now call `topic.*Table()` and carry the schema; the 51
   shared-table references are qualified too (37 raw literals became
   `fmt.Sprintf`, 8 already were, and 6 helpers in invariantlab and
   schemagatelab gained a `schema` param, since they take a bare pool).
   Zero unqualified references remain in examples/.
   The 62 display strings (`die("delivery_log_5 has %d rows")`) stay
   bare: they name a table for a human, not for Postgres.
   The sweep broke 8 labs, all one mistake -- a blanket qualification
   flattens the distinction task 4 got right in production. The bare name
   is required wherever it is compared to a catalog `relname` (topiclab,
   dropfloorlab, compactionheadwritelab), matched against EXPLAIN output,
   which prints partition names unqualified (partitionlab and three
   compaction labs), or used as an `ALTER TABLE ... RENAME TO` target,
   which Postgres refuses to qualify (dutybackofflab). All 8 pass with
   the two names kept apart.
9. ~~**Verification.**~~ DONE. Fresh-DB suite 47/47, `just compat-lab`
   round-trip, `just verify`, tools 24 conventions tests, and
   `npm run verify` (vale 0 errors/94 files, vitest 114, Playwright 18
   across three engines).
   `npm run verify` caught a real task-6 bug the unit tests could not:
   `getGroupSql` and `registerGroupInsertSql` needed no interpolation
   until the re-sync gave them `%[1]s.consumer_group_config`, and
   database.ts used them raw, so a literal `%[1]s` reached PGlite and the
   prerender of `/` failed with `syntax error at or near "%"`. Both are
   now a `*SqlTemplate` plus a filled function, and a test asserts
   database.ts names no `*SqlTemplate` at all -- executing a template is
   the bug, so the guard is structural rather than a value check.

**search_path stays `"<schema>, public"`.** With every literal qualified
the leading entry is belt-and-braces, and dropping it would even fix
[0630]'s noted wart -- a caller's `CREATE TABLE` inside `InTransaction`
would land in their own schema rather than Vulkan's. It stays anyway,
because the qualification walk sees only pkg/ and cmd/ literals: a
post-v1 migration step reaches no schema at all (its funcs take
`(ctx, q, topicId)`), so without the entry its SQL would silently read
and create in `public`. Revisit when that gap closes; the InTransaction
improvement is the payoff.
