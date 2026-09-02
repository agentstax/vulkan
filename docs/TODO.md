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
