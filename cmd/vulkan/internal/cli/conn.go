package cli

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"github.com/agentstax/vulkan/pkg/common/logging"
	"github.com/agentstax/vulkan/pkg/datastore"
	vulkan "github.com/agentstax/vulkan/pkg/vulkan"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	databaseURLEnv = "VULKAN_ADMIN_DATABASE_URL"
	schemaEnv      = "VULKAN_ADMIN_SCHEMA"
)

// openDatastore resolves the connection (flag then env) and dials Postgres.
// pgx owns the DSN, so sslmode, pool_max_conns, connect_timeout, the
// keyword/value form, and the libpq PG* environment variables all work
// without this package parsing any of them.
// An empty schema is left to PostgresDatastoreConfig.WithDefaults.
// The returned close func releases the pool; callers defer it.
func openDatastore(ctx context.Context, databaseURL string, schema string) (*datastore.PostgresDatastore, func(), error) {
	raw := databaseURL
	if raw == "" {
		raw = os.Getenv(databaseURLEnv)
	}
	if raw == "" {
		return nil, nil, failUsage("no database URL -- pass --database-url or set %s", databaseURLEnv)
	}
	if schema == "" {
		schema = os.Getenv(schemaEnv)
	}

	datastoreConfig := &datastore.PostgresDatastoreConfig{Schema: schema}
	datastoreConfig.WithDefaults()
	if err := datastoreConfig.Validate(); err != nil {
		return nil, nil, failUsage("%s", err.Error())
	}

	// the wrong scheme is the common paste error and deserves a better message
	// than pgx's; a keyword/value DSN (host=... user=...) carries no scheme and
	// goes straight through
	if scheme, _, found := strings.Cut(raw, "://"); found && scheme != "postgres" && scheme != "postgresql" {
		return nil, nil, failUsage("database URL must start with postgres:// or postgresql:// (got %q)", scheme)
	}

	// the DSN is user input, so failing to parse it is a usage error --
	// everything past this point is the database being unreachable
	poolConfig, err := pgxpool.ParseConfig(raw)
	if err != nil {
		return nil, nil, failUsage("could not parse database URL: %v", err)
	}

	// search_path selects nothing: every vulkan statement names its own
	// schema, so a DSN carrying one silently reads the default installation
	if _, ok := poolConfig.ConnConfig.RuntimeParams["search_path"]; ok {
		return nil, nil, failUsage("database URL sets search_path -- pass --schema or set %s instead", schemaEnv)
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, nil, failOp("could not connect to database: %v", err)
	}

	ds, err := datastore.NewPostgresDatastore(ctx, pool, datastoreConfig)
	if err != nil {
		pool.Close()
		return nil, nil, failOp("could not connect to database: %v", err)
	}
	return ds, func() { pool.Close() }, nil
}

// openClient is openDatastore plus a Client. AllowDestroy is set here
// because this binary IS the privileged admin tool -- the gate exists for
// library embedders, not the CLI (ADMIN_CLI.md). The datastore is returned
// too, so destroy can build a topic controller for the one thing the
// client doesn't expose (an emptiness probe).
func openClient(ctx context.Context, databaseURL string, schema string) (*vulkan.Client, *datastore.PostgresDatastore, func(), error) {
	ds, closeDS, err := openDatastore(ctx, databaseURL, schema)
	if err != nil {
		return nil, nil, nil, err
	}

	// Library logs go to stderr (never stdout, which carries the command payload)
	// and only at ERROR: the library's routine INFO/WARN lines ("topic
	// registered", "topic destroyed") are implementation noise here -- the CLI's
	// own ✓/error output is the interface.
	client, err := vulkan.NewClient(ds, &vulkan.ClientConfig{
		AllowDestroy: true,
		Logger:       logging.NewDefaultLogger(os.Stderr, slog.LevelError),
	})
	if err != nil {
		closeDS()
		return nil, nil, nil, failOp("could not initialize client: %v", err)
	}

	return client, ds, closeDS, nil
}
