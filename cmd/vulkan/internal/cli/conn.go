package cli

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"

	"github.com/agentstax/vulkan/pkg/admin"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/logger"
)

const databaseURLEnv = "VULKAN_ADMIN_DATABASE_URL"

// parseConnConfig turns a postgres:// URL into the struct pkg/datastore takes.
// pkg/datastore has no URL constructor today, so the CLI owns the parse -- see
// ADMIN_CLI.md's connection-wiring caveat. Query params beyond the core fields
// (sslmode et al.) are dropped by that struct; we warn rather than drop silent,
// since an ignored sslmode is a security footgun, not a cosmetic loss.
func parseConnConfig(raw string) (*datastore.PostgresConnectionConfig, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, failUsage("could not parse database URL: %v", err)
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return nil, failUsage("database URL must start with postgres:// or postgresql:// (got %q)", u.Scheme)
	}

	cfg := &datastore.PostgresConnectionConfig{
		Host:     u.Hostname(),
		Database: pathDatabase(u),
	}
	if u.User != nil {
		cfg.User = u.User.Username()
		if pass, ok := u.User.Password(); ok {
			cfg.Pass = pass
		}
	}
	if p := u.Port(); p != "" {
		port, err := strconv.Atoi(p)
		if err != nil {
			return nil, failUsage("database URL has a non-numeric port %q", p)
		}
		cfg.Port = port
	}

	for key := range u.Query() {
		if key == "pool_max_conns" {
			continue
		}
		fmt.Fprintf(os.Stderr, "warning: database URL parameter %q is not supported yet and was ignored\n", key)
	}

	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, failUsage("%s", err.Error())
	}
	return cfg, nil
}

func pathDatabase(u *url.URL) string {
	if len(u.Path) > 0 && u.Path[0] == '/' {
		return u.Path[1:]
	}
	return u.Path
}

// openAdmin resolves the connection (flag then env), dials Postgres, and builds
// a MessageAdmin. AllowDestroy is set here because this binary IS the privileged
// admin tool -- the gate exists for library embedders, not the CLI (ADMIN_CLI.md).
// The datastore is returned too, so destroy can build a topic.TopicDatastore for
// the one thing MessageAdmin doesn't expose (an emptiness probe). The returned
// close func releases the pool; callers defer it.
func openAdmin(ctx context.Context, databaseURL string) (*admin.MessageAdmin, *datastore.PostgresDatastore, func(), error) {
	raw := databaseURL
	if raw == "" {
		raw = os.Getenv(databaseURLEnv)
	}
	if raw == "" {
		return nil, nil, nil, failUsage("no database URL -- pass --database-url or set %s", databaseURLEnv)
	}

	cfg, err := parseConnConfig(raw)
	if err != nil {
		return nil, nil, nil, err
	}

	ds, err := datastore.NewPostgresDatastore(ctx, cfg)
	if err != nil {
		return nil, nil, nil, failOp("could not connect to database: %v", err)
	}

	// Library logs go to stderr (never stdout, which carries --json/-q payload)
	// and only at ERROR: the library's routine INFO/WARN lines ("topic
	// registered", "topic destroyed") are implementation noise here -- the CLI's
	// own ✓/error output is the interface.
	mAdmin, err := admin.NewMessageAdmin(ds, &admin.MessageAdminConfig{
		AllowDestroy: true,
		Logger:       logger.NewDefaultLogger(os.Stderr, slog.LevelError),
	})
	if err != nil {
		ds.Close()
		return nil, nil, nil, failOp("could not initialize admin: %v", err)
	}

	return mAdmin, ds, func() { ds.Close() }, nil
}
