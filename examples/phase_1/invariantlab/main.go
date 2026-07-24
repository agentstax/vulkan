package main

// invariant lab: the migrate engine's guarantees, exercised with a FIXTURE
// registry (the real registries are empty). This is the linear-history
// enforcement golang-migrate's file layout used to give for free.
//
// It borrows the SYSTEM entity against throwaway scratch tables and resets the
// system schema_log to its v1 baseline on exit -- nothing real is touched.
//
// Proves:
//  1. migrating v1->N produces the SAME schema as creating N fresh -- a
//     Down-doesn't-invert-Up or baseline-drift bug surfaces as a column diff.
//  2. up->down->up round-trips: Down reverts Up, re-up reproduces the schema.
//  3. Up and Down are idempotent -- re-run against already-applied state (the
//     ambiguous-commit retry), they no-op instead of erroring.

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/agentstax/vulkan/pkg/admin"
	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/migrate"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	stepwise = "invariantlab_stepwise" // built version-by-version via the fixture steps
	fresh    = "invariantlab_fresh"    // created in its final shape directly
	maxV     = 4
)

func main() {
	ctx := context.Background()

	ds, err := coredatastore.NewPostgresDatastore(ctx, &coredatastore.PostgresConnectionConfig{
		User: "example_user", Pass: "example_password",
		Host: "localhost", Port: 5432, Database: "example_db",
	})
	must(err)
	defer ds.Close()
	pool := ds.Pool

	mAdmin, err := admin.NewMessageAdmin(ds, nil)
	must(err)
	must(mAdmin.RegisterSystem(ctx))

	runner, err := migrate.NewRunner(ds, nil, logger.NewDefaultLogger(os.Stderr, slog.LevelError))
	must(err)
	reg := fixture()

	reset(ctx, pool) // clear leftovers from any prior crashed run
	defer reset(ctx, pool)

	// 1. fresh == migrate ------------------------------------------------------
	section("migrate v1 -> v4 builds the same schema as a fresh create-at-4")
	must(runner.Run(ctx, maxV, migrate.EntitySystem, 0, reg))
	must(createFresh(ctx, pool))
	check(sameColumns(ctx, pool), "stepwise migration == fresh-create-at-4 (information_schema)")

	// 2. up -> down -> up ------------------------------------------------------
	section("Down inverts Up, and re-up reproduces the schema")
	must(runner.Run(ctx, 1, migrate.EntitySystem, 0, reg))
	check(!tableExists(ctx, pool, stepwise), "full down dropped the table")
	must(runner.Run(ctx, maxV, migrate.EntitySystem, 0, reg))
	check(sameColumns(ctx, pool), "re-up reproduced the identical schema")

	// 3. Up idempotency: version says v3 but the DDL is already at v4, so the
	// re-run re-applies step 4's Up against an object that already exists.
	section("Up is idempotent under an ambiguous-commit re-run")
	forgetVersion(ctx, pool, maxV)
	must(runner.Run(ctx, maxV, migrate.EntitySystem, 0, reg))
	check(currentVersion(ctx, pool) == maxV && sameColumns(ctx, pool),
		"re-applied Up over existing schema -> no-op, schema unchanged")

	// 4. Down idempotency: drop c3 (now at v3), then claim v4 again so the
	// re-run re-applies step 4's Down against a column that's already gone.
	section("Down is idempotent under an ambiguous-commit re-run")
	must(runner.Run(ctx, maxV-1, migrate.EntitySystem, 0, reg))
	claimVersion(ctx, pool, maxV)
	must(runner.Run(ctx, maxV-1, migrate.EntitySystem, 0, reg))
	check(currentVersion(ctx, pool) == maxV-1 && !hasColumn(ctx, pool, stepwise, "c3"),
		"re-applied Down over absent column -> no-op")

	fmt.Println("\n✅ INVARIANT LAB PASSED")
	fmt.Println("   migrate-to-N == fresh-at-N; Down inverts Up; both directions idempotent.")
}

// fixture builds a 3-column table across versions 2..4. Every step is
// idempotent (IF [NOT] EXISTS) -- the engine may re-run one on a transient blip.
func fixture() []migrate.Migration {
	return []migrate.Migration{
		{Version: 2,
			Up:   exec(`CREATE TABLE IF NOT EXISTS ` + stepwise + ` (id BIGINT, c1 TEXT);`),
			Down: exec(`DROP TABLE IF EXISTS ` + stepwise + `;`)},
		{Version: 3,
			Up:   exec(`ALTER TABLE ` + stepwise + ` ADD COLUMN IF NOT EXISTS c2 INT;`),
			Down: exec(`ALTER TABLE ` + stepwise + ` DROP COLUMN IF EXISTS c2;`)},
		{Version: 4,
			Up:   exec(`ALTER TABLE ` + stepwise + ` ADD COLUMN IF NOT EXISTS c3 BOOLEAN;`),
			Down: exec(`ALTER TABLE ` + stepwise + ` DROP COLUMN IF EXISTS c3;`)},
	}
}

func exec(sql string) func(context.Context, coredatastore.Querier, int64) error {
	return func(ctx context.Context, q coredatastore.Querier, _ int64) error {
		_, err := q.Exec(ctx, sql)
		return err
	}
}

func createFresh(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS `+fresh+` (id BIGINT, c1 TEXT, c2 INT, c3 BOOLEAN);`)
	return err
}

// sameColumns diffs the two tables by name + type in ordinal order -- a
// Down-doesn't-invert-Up or a baseline-that-drifted-from-the-steps shows here.
func sameColumns(ctx context.Context, pool *pgxpool.Pool) bool {
	return equal(columns(ctx, pool, stepwise), columns(ctx, pool, fresh))
}

func columns(ctx context.Context, pool *pgxpool.Pool, table string) []string {
	rows, err := pool.Query(ctx,
		`SELECT column_name, data_type FROM information_schema.columns WHERE table_name = $1 ORDER BY ordinal_position;`, table)
	must(err)
	defer rows.Close()

	var cols []string
	for rows.Next() {
		var name, typ string
		must(rows.Scan(&name, &typ))
		cols = append(cols, name+":"+typ)
	}
	must(rows.Err())
	return cols
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func tableExists(ctx context.Context, pool *pgxpool.Pool, table string) bool {
	var exists bool
	must(pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = $1);`, table).Scan(&exists))
	return exists
}

func hasColumn(ctx context.Context, pool *pgxpool.Pool, table, col string) bool {
	var exists bool
	must(pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = $1 AND column_name = $2);`, table, col).Scan(&exists))
	return exists
}

func currentVersion(ctx context.Context, pool *pgxpool.Pool) int64 {
	var v int64
	must(pool.QueryRow(ctx, `SELECT schema_version FROM schema_log WHERE entity_type = 'system' AND entity_id = 0 AND status = 'success' ORDER BY id DESC LIMIT 1;`).Scan(&v))
	return v
}

// forgetVersion drops the success records at/above v, so the engine reads the
// current version as v-1 while the DDL is already at v -- an interrupted migrate.
func forgetVersion(ctx context.Context, pool *pgxpool.Pool, v int64) {
	_, err := pool.Exec(ctx, `DELETE FROM schema_log WHERE entity_type = 'system' AND entity_id = 0 AND schema_version >= $1;`, v)
	must(err)
}

// claimVersion records a success at v without doing v's DDL -- the mirror of
// forgetVersion, so the engine believes it's ahead of where the schema is.
func claimVersion(ctx context.Context, pool *pgxpool.Pool, v int64) {
	_, err := pool.Exec(ctx, `INSERT INTO schema_log (entity_type, entity_id, schema_version, status) VALUES ('system', 0, $1, 'success');`, v)
	must(err)
}

// reset drops the scratch tables and returns the system schema_log to exactly
// one v1 baseline row -- the lab only ever borrowed the system entity, and its
// round trips leave extra v1 rows (each down-to-baseline records one).
func reset(ctx context.Context, pool *pgxpool.Pool) {
	_, err := pool.Exec(ctx, `DROP TABLE IF EXISTS `+stepwise+`, `+fresh+`;`)
	must(err)
	_, err = pool.Exec(ctx, `DELETE FROM schema_log WHERE entity_type = 'system' AND entity_id = 0;`)
	must(err)
	_, err = pool.Exec(ctx, `INSERT INTO schema_log (entity_type, entity_id, schema_version, status) VALUES ('system', 0, 1, 'success');`)
	must(err)
}

func section(title string) { fmt.Printf("\n--- %s ---\n", title) }

func check(cond bool, msg string) {
	if !cond {
		fmt.Printf("  ✗ %s\n", msg)
		os.Exit(1)
	}
	fmt.Printf("  ✓ %s\n", msg)
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
