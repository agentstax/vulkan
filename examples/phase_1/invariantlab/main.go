package main

// invariant lab: the migrate engine's guarantees, exercised with a FIXTURE
// registry (the real registries are empty). This is the linear-history
// enforcement golang-migrate's file layout used to give for free.
//
// It borrows the SYSTEM scope against throwaway scratch tables and resets the
// system migration_log to its v1 baseline on exit -- nothing real is touched.
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

	"github.com/agentstax/vulkan/pkg/common/logging"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/migrate"
	migratecontroller "github.com/agentstax/vulkan/pkg/migrate/controller"
	vulkan "github.com/agentstax/vulkan/pkg/vulkan"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	stepwise = "invariantlab_stepwise" // built version-by-version via the fixture steps
	fresh    = "invariantlab_fresh"    // created in its final shape directly
	maxV     = 4
)

func main() {
	if err := run(); err != nil {
		fmt.Printf("\n❌ LAB FAILED: %s\n", err.Error())
		os.Exit(1)
	}
}

// labFailure is what die panics with; run recovers it into its error so
// main's deferred cleanup runs on a failed assertion.
type labFailure struct {
	message string
}

func (f labFailure) Error() string {
	return f.message
}

func run() (err error) {
	defer func() {
		switch recovered := recover().(type) {
		case nil:
		case labFailure:
			err = recovered
		default:
			panic(recovered)
		}
	}()
	ctx := context.Background()

	pool, err := vulkan.NewPostgresPool(ctx, "example_user", "example_password", "localhost", "example_db", nil)
	must(err)
	defer pool.Close()

	client, err := vulkan.NewClient(ctx, pool, nil)
	must(err)
	ds := client.Datastore()
	must(client.System().Register(ctx, nil))

	controller, err := migratecontroller.NewController(ds, &migratecontroller.ControllerConfig{Logger: logging.NewDefaultLogger(os.Stderr, slog.LevelError)})
	must(err)
	reg := fixture()

	sysOwner, err := controller.SystemOwner(ctx)
	must(err)
	sysId := sysOwner.SystemId

	reset(ctx, pool, ds.Schema, sysId) // clear leftovers from any prior crashed run
	defer reset(ctx, pool, ds.Schema, sysId)

	// 1. fresh == migrate ------------------------------------------------------
	section("migrate v1 -> v4 builds the same schema as a fresh create-at-4")
	must(controller.RunOnce(ctx, maxV, sysOwner, reg))
	must(createFresh(ctx, pool, ds.Schema))
	check(sameColumns(ctx, pool, ds.Schema), "stepwise migration == fresh-create-at-4 (information_schema)")

	// 2. up -> down -> up ------------------------------------------------------
	section("Down inverts Up, and re-up reproduces the schema")
	must(controller.RunOnce(ctx, 1, sysOwner, reg))
	check(!tableExists(ctx, pool, ds.Schema, stepwise), "full down dropped the table")
	must(controller.RunOnce(ctx, maxV, sysOwner, reg))
	check(sameColumns(ctx, pool, ds.Schema), "re-up reproduced the identical schema")

	// 3. Up idempotency: version says v3 but the DDL is already at v4, so the
	// re-run re-applies step 4's Up against an object that already exists.
	section("Up is idempotent under an ambiguous-commit re-run")
	forgetVersion(ctx, pool, ds.Schema, sysId, maxV)
	must(controller.RunOnce(ctx, maxV, sysOwner, reg))
	check(currentVersion(ctx, pool, ds.Schema, sysId) == maxV && sameColumns(ctx, pool, ds.Schema),
		"re-applied Up over existing schema -> no-op, schema unchanged")

	// 4. Down idempotency: drop c3 (now at v3), then claim v4 again so the
	// re-run re-applies step 4's Down against a column that's already gone.
	section("Down is idempotent under an ambiguous-commit re-run")
	must(controller.RunOnce(ctx, maxV-1, sysOwner, reg))
	claimVersion(ctx, pool, ds.Schema, sysId, maxV)
	must(controller.RunOnce(ctx, maxV-1, sysOwner, reg))
	check(currentVersion(ctx, pool, ds.Schema, sysId) == maxV-1 && !hasColumn(ctx, pool, ds.Schema, stepwise, "c3"),
		"re-applied Down over absent column -> no-op")

	fmt.Println("\n✅ INVARIANT LAB PASSED")
	fmt.Println("   migrate-to-N == fresh-at-N; Down inverts Up; both directions idempotent.")
	return nil
}

// fixture builds a 3-column table across versions 2..4. Every step is
// idempotent (IF [NOT] EXISTS) -- the engine may re-run one on a transient blip.
func fixture() []migrate.Migration {
	return []migrate.Migration{
		{Version: 2,
			Up:   exec(`CREATE TABLE IF NOT EXISTS %s.` + stepwise + ` (id BIGINT, c1 TEXT);`),
			Down: exec(`DROP TABLE IF EXISTS %s.` + stepwise + `;`)},
		{Version: 3,
			Up:   exec(`ALTER TABLE %s.` + stepwise + ` ADD COLUMN IF NOT EXISTS c2 INT;`),
			Down: exec(`ALTER TABLE %s.` + stepwise + ` DROP COLUMN IF EXISTS c2;`)},
		{Version: 4,
			Up:   exec(`ALTER TABLE %s.` + stepwise + ` ADD COLUMN IF NOT EXISTS c3 BOOLEAN;`),
			Down: exec(`ALTER TABLE %s.` + stepwise + ` DROP COLUMN IF EXISTS c3;`)},
	}
}

// exec fills the statement's %s with the schema the engine hands the step --
// a step reaches no datastore, so this is the only way its SQL names a table
func exec(sql string) func(context.Context, iDatastore.Querier, string, int64) error {
	return func(ctx context.Context, q iDatastore.Querier, schema string, _ int64) error {
		_, err := q.Exec(ctx, fmt.Sprintf(sql, schema))
		return err
	}
}

func createFresh(ctx context.Context, pool *pgxpool.Pool, schema string) error {
	_, err := pool.Exec(ctx, fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.`+fresh+` (id BIGINT, c1 TEXT, c2 INT, c3 BOOLEAN);`, schema))
	return err
}

// sameColumns diffs the two tables by name + type in ordinal order -- a
// Down-doesn't-invert-Up or a baseline-that-drifted-from-the-steps shows here.
func sameColumns(ctx context.Context, pool *pgxpool.Pool, schema string) bool {
	return equal(columns(ctx, pool, schema, stepwise), columns(ctx, pool, schema, fresh))
}

func columns(ctx context.Context, pool *pgxpool.Pool, schema string, table string) []string {
	rows, err := pool.Query(ctx,
		`SELECT column_name, data_type FROM information_schema.columns WHERE table_schema = $1 AND table_name = $2 ORDER BY ordinal_position;`, schema, table)
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

func tableExists(ctx context.Context, pool *pgxpool.Pool, schema string, table string) bool {
	var exists bool
	must(pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = $1 AND table_name = $2);`, schema, table).Scan(&exists))
	return exists
}

func hasColumn(ctx context.Context, pool *pgxpool.Pool, schema string, table string, col string) bool {
	var exists bool
	must(pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = $1 AND table_name = $2 AND column_name = $3);`, schema, table, col).Scan(&exists))
	return exists
}

func currentVersion(ctx context.Context, pool *pgxpool.Pool, schema string, sysId int64) int64 {
	var v int64
	must(pool.QueryRow(ctx, fmt.Sprintf(`SELECT migration_version FROM %s.migration_log WHERE system_id = $1 AND status = 'success' ORDER BY id DESC LIMIT 1;`, schema), sysId).Scan(&v))
	return v
}

// forgetVersion drops the success records at/above v, so the engine reads the
// current version as v-1 while the DDL is already at v -- an interrupted migrate.
func forgetVersion(ctx context.Context, pool *pgxpool.Pool, schema string, sysId int64, v int64) {
	_, err := pool.Exec(ctx, fmt.Sprintf(`DELETE FROM %s.migration_log WHERE system_id = $1 AND migration_version >= $2;`, schema), sysId, v)
	must(err)
}

// claimVersion records a success at v without doing v's DDL -- the mirror of
// forgetVersion, so the engine believes it's ahead of where the schema is.
func claimVersion(ctx context.Context, pool *pgxpool.Pool, schema string, sysId int64, v int64) {
	_, err := pool.Exec(ctx, fmt.Sprintf(`INSERT INTO %s.migration_log (system_id, migration_version, status) VALUES ($1, $2, 'success');`, schema), sysId, v)
	must(err)
}

// reset drops the scratch tables and returns the system migration_log to exactly
// one v1 baseline row -- the lab only ever borrowed the system scope, and its
// round trips leave extra v1 rows (each down-to-baseline records one).
func reset(ctx context.Context, pool *pgxpool.Pool, schema string, sysId int64) {
	_, err := pool.Exec(ctx, fmt.Sprintf(`DROP TABLE IF EXISTS %[1]s.`+stepwise+`, %[1]s.`+fresh+`;`, schema))
	must(err)
	_, err = pool.Exec(ctx, fmt.Sprintf(`DELETE FROM %s.migration_log WHERE system_id = $1;`, schema), sysId)
	must(err)
	_, err = pool.Exec(ctx, fmt.Sprintf(`INSERT INTO %s.migration_log (system_id, migration_version, status) VALUES ($1, 1, 'success');`, schema), sysId)
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
		die(err.Error())
	}
}

func die(msg string) {
	panic(labFailure{message: msg})
}
