package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/agentstax/vulkan/pkg/common/logging"
	"github.com/agentstax/vulkan/pkg/datastore"
	vulkan "github.com/agentstax/vulkan/pkg/vulkan"
)

const databaseURLEnv = "VULKAN_ADMIN_DATABASE_URL"

// connection is parseConnConfig's result: the required values
// datastore.NewPostgresDatastore takes as params, plus the optional knobs.
type connection struct {
	User     string
	Host     string
	Database string
	Config   *datastore.PostgresConnectionConfig
}

func newConnection(user string, host string, database string, config *datastore.PostgresConnectionConfig) (*connection, error) {
	if config == nil {
		return nil, errors.New("config must not be nil")
	}
	return &connection{User: user, Host: host, Database: database, Config: config}, nil
}

// parseConnConfig turns a postgres:// URL into what pkg/datastore takes.
// pkg/datastore has no URL constructor today, so the CLI owns the parse -- see
// ADMIN_CLI.md's connection-wiring caveat. pool_max_conns and connect_timeout
// map onto MaxConns/ConnectTimeout -- plain scalars, safe to parse here.
// TLSConfig has no such mapping: it's a real *tls.Config for embedders
// building one in code (mirroring pgconn.Config), not something a URL query
// param can produce without reimplementing pgx's own sslmode/cert negotiation
// -- so sslmode and any other unrecognized param are warned about, not
// silently dropped.
func parseConnConfig(raw string) (*connection, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, failUsage("could not parse database URL: %v", err)
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return nil, failUsage("database URL must start with postgres:// or postgresql:// (got %q)", u.Scheme)
	}

	// the URL is user input, so missing required parts are usage errors here,
	// not dial-time errors from the constructor
	if u.Hostname() == "" {
		return nil, failUsage("database URL has no host")
	}
	if pathDatabase(u) == "" {
		return nil, failUsage("database URL has no database name")
	}
	if u.User == nil || u.User.Username() == "" {
		return nil, failUsage("database URL has no user")
	}

	cfg := &datastore.PostgresConnectionConfig{}
	if pass, ok := u.User.Password(); ok {
		cfg.Pass = pass
	}
	if p := u.Port(); p != "" {
		port, err := strconv.Atoi(p)
		if err != nil {
			return nil, failUsage("database URL has a non-numeric port %q", p)
		}
		cfg.Port = port
	}

	for key, vals := range u.Query() {
		switch key {
		case "pool_max_conns":
			maxConns, err := strconv.Atoi(vals[0])
			if err != nil {
				return nil, failUsage("database URL has a non-numeric pool_max_conns %q", vals[0])
			}
			cfg.MaxConns = maxConns
		case "connect_timeout":
			secs, err := strconv.Atoi(vals[0])
			if err != nil {
				return nil, failUsage("database URL has a non-numeric connect_timeout %q", vals[0])
			}
			cfg.ConnectTimeout = time.Duration(secs) * time.Second
		default:
			fmt.Fprintf(os.Stderr, "warning: database URL parameter %q is not supported yet and was ignored\n", key)
		}
	}

	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, failUsage("%s", err.Error())
	}
	return newConnection(u.User.Username(), u.Hostname(), pathDatabase(u), cfg)
}

func pathDatabase(u *url.URL) string {
	if len(u.Path) > 0 && u.Path[0] == '/' {
		return u.Path[1:]
	}
	return u.Path
}

// openDatastore resolves the connection (flag then env) and dials Postgres.
// The returned close func releases the pool; callers defer it.
func openDatastore(ctx context.Context, databaseURL string) (*datastore.PostgresDatastore, func(), error) {
	raw := databaseURL
	if raw == "" {
		raw = os.Getenv(databaseURLEnv)
	}
	if raw == "" {
		return nil, nil, failUsage("no database URL -- pass --database-url or set %s", databaseURLEnv)
	}

	conn, err := parseConnConfig(raw)
	if err != nil {
		return nil, nil, err
	}

	ds, err := datastore.NewPostgresDatastore(ctx, conn.User, conn.Host, conn.Database, conn.Config)
	if err != nil {
		return nil, nil, failOp("could not connect to database: %v", err)
	}
	return ds, func() { ds.Close() }, nil
}

// openClient is openDatastore plus a Client. AllowDestroy is set here
// because this binary IS the privileged admin tool -- the gate exists for
// library embedders, not the CLI (ADMIN_CLI.md). The datastore is returned
// too, so destroy can build a topic controller for the one thing the
// client doesn't expose (an emptiness probe).
func openClient(ctx context.Context, databaseURL string) (*vulkan.Client, *datastore.PostgresDatastore, func(), error) {
	ds, closeDS, err := openDatastore(ctx, databaseURL)
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
