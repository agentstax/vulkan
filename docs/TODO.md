# TODO

Sliding window of in-flight work only. Future work lives in ROADMAP.md;
shipped work in HISTORY.md; decision rationale in DECISIONS.md ->
docs/decisions/.

## The datastore takes the caller's pool [0633]

`NewPostgresDatastore(ctx, pool, cfg)` is the one entry point and
`NewPostgresPool(ctx, user, password, host, database, cfg)` is the
guided builder beside it. Whoever builds the pool closes it.

Why: the shipped signature splits the credential pair (`user` inline,
`Pass` in the config, reading as though a password were optional tuning
next to `MaxConns`), re-spells four knobs pgx already parses, and gives
an application that already runs pgx no way to hand over the pool it
has. The doc page is written and marked Proposed -- `## Proposed: the
datastore takes your pool` in `website/src/content/docs/guides/client.mdx`
is the reviewed spec for everything below.

### Tasks

1. ~~**The two constructors.**~~ DONE. `NewPostgresDatastore(ctx, pool,
   *PostgresDatastoreConfig)` keeps the ping and no longer closes what it
   did not open; `NewPostgresPool(ctx, user, password, host, database,
   *PostgresConnectionConfig)` is pure assembly. `Close` deleted, the
   config split across `datastore_config.go` (Schema) and
   `connection_config.go` (Port, MaxConns, ConnectTimeout, TLSConfig).
   Fixed on the way past, since the DSN was being rebuilt anyway: the old
   `fmt.Sprintf("postgres://%s:%s@%s:%s/%s", ...)` corrupted any password
   holding `@`, `:`, `/`, or `#` and produced a broken authority for an
   IPv6 host. It now builds through `net/url` + `net.JoinHostPort`, with
   `datastore_test.go` round-tripping four cases back through
   `pgxpool.ParseConfig`.
2. ~~**The CLI's URL parser goes.**~~ DONE. `parseConnConfig`, the
   `connection` struct, `newConnection`, and `pathDatabase` deleted, ~90
   lines, in favour of `pgxpool.ParseConfig` + `NewWithConfig`. Confirmed
   empirically before deleting: pgx reads `pool_max_conns`,
   `connect_timeout`, and `sslmode` (which the old code warned it could
   not honour) natively, accepts the keyword/value DSN form, and defaults
   the user from the environment the way libpq does. Classification
   preserved -- a parse failure is `failUsage`, dialing is `failOp`, and
   the schema is defaulted and validated before the pool is built so a bad
   `--schema` stays a usage error. The `search_path` guard survives,
   reading `ConnConfig.RuntimeParams` instead of re-parsing the URL, with
   its reason updated: a DSN's path selects nothing now that every
   statement names its own schema.
3. ~~**Call-site sweep.**~~ DONE. 75 constructor calls, 74 `defer
   ds.Close()` -> `defer pool.Close()`. Mostly scripted; four things the
   script could not do: the two bench drivers build their arguments from
   `envOr(...)` rather than literals, `invariantlab` and `schemagatelab`
   already held a `pool := ds.Pool` alias that became a redeclaration
   (deleted -- it was always redundant), and `schemalab` closes four
   named datastores from a helper that returns no pool, so those go
   through the exported `Pool` field. All seven modules build and vet.
4. ~~**The manager's start line reports the pool's max.**~~ DONE.
   `ManagerProvisioner` reads `ds.Pool.Config().MaxConns` once at
   construction and `ManagerInstance` prints it as `pool_max_conns`.
   Registry row added to CONVENTIONS ## Logging in the same change.
5. ~~**Docs.**~~ DONE. The client guide's Proposed section became
   `## Building the pool` and its two samples take the pool; the
   quickstart's two programs were rewritten onto `main` -> `run() error`,
   which retired the banned `must()` helper there, and both were extracted
   and compiled against the working tree. `VK0064` moved to
   `PostgresDatastoreConfig`. The straggler grep found one more
   [0632] leftover: VK0064 still said the schema name "reaches CREATE
   SCHEMA and the connection's search_path", now the statement qualifier.
6. ~~**Verify.**~~ DONE. `just verify` green across all seven modules;
   fresh-DB lab suite 47/47. The CLI's rewritten connection path was
   driven by hand for the cases no lab covers: a basic URL lists topics,
   a wrong scheme and a DSN carrying `search_path` still fail as usage
   errors, and `sslmode=disable` plus the keyword/value DSN form now work
   where the old parser warned or could not parse. Task 4 confirmed in a
   real run rather than by reading -- metrics-collector-lab's log carries
   `manager instance starting ... rate=1s pool_max_conns=10`.

Ready for closeout: HISTORY.md entry, then these lines and the ROADMAP
round-1 pointer come out.

Worth knowing at the next release checkpoint: `tools/compat` drives the
PINNED release's API, and its go.mod currently replaces vulkan with the
working tree, which is why it compiles today. The moment it pins a real
tag from before this change, `main.go` will not build against it -- the
constructor it calls does not exist there. That is the compat lab
working, not breaking: [0633] is a breaking API change, taken pre-v1.
