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

1. **The two constructors.** `NewPostgresDatastore` takes
   `pool *pgxpool.Pool` and a new `PostgresDatastoreConfig` holding
   `Schema` alone; it keeps its `ctx` and its single `Ping`, so a bad
   address still fails at construction. `NewPostgresPool` takes the
   exploded DSN in URL order with `password` inline (`""` = none) and
   keeps `PostgresConnectionConfig` trimmed to `Port`, `MaxConns`,
   `ConnectTimeout`, `TLSConfig` -- pure assembly over
   `pgxpool.NewWithConfig`, no ping. `PostgresDatastore.Close` is
   deleted (`pkg/datastore/datastore.go:81`).
2. **The CLI's URL parser goes.** `cmd/vulkan/internal/cli/conn.go`
   hand-parses `postgres://` URLs only because "pkg/datastore has no URL
   constructor today" -- `parseConnConfig`, the `connection` struct, and
   `pathDatabase` all delete in favour of `pgxpool.New(ctx, raw)`. The
   CLI gets strictly more capable: it warns today that it cannot honour
   `sslmode`, and pgx parses it natively along with `pool_max_conns` and
   `connect_timeout`. Two things to preserve: usage errors stay usage
   errors (a malformed URL is `failUsage`, a dial failure is `failOp`),
   and `VULKAN_ADMIN_SCHEMA` moves onto the datastore config.
3. **Call-site sweep.** 75 `NewPostgresDatastore` calls -- 58
   `examples/phase_1`, 13 `examples/playground`, and one each in
   `cmd/vulkan`, `tools/compat`, `bench/fillfactor`, `bench/compaction`
   -- plus 74 `ds.Close()` becoming `defer pool.Close()` and 85
   `PostgresConnectionConfig` references. `tools/compat` pins a prior release, so its pin flow in
   go.mod needs re-reading before it is touched.
4. **The manager's start line reports the pool's max.** `MaxConns`
   carries the only warning that pgx's default pool is `max(4, numCPU)`
   and quietly caps a high worker count; nothing can enforce that
   against a pool Vulkan did not build, so the fact moves to the start
   line as a config attribute an operator can read.
5. **Docs.** Flip the client guide's Connect section and `## The schema`
   sample onto the new form and drop the Proposed heading; rewrite the
   quickstart's samples (both carry the constructor, and while every
   sample there is being touched, drop the banned `must()` helper).
   `errors/VK0064.md` names the constructor too. While in the site:
   grep for other [0630]-[0632] stragglers -- two were already found and
   fixed 2026-09-02 (the client guide claimed the pool sets a
   `search_path` that [0632] removed, and the quickstart claimed tables
   land in the database's default schema rather than `vulkan`), so the
   sweep is not hypothetical.
6. **Verify.** Build plus `go test -race` on touched packages and the
   directly-affected labs per change; the full fresh-DB lab suite at the
   review-ready checkpoint, since the sweep touches all 58 phase_1 labs.
