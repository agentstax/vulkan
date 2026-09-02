package datastore

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresDatastore struct {
	Pool *pgxpool.Pool

	// Schema is the namespace every vulkan table lives in
	Schema string
}

// NewPostgresDatastore opens a connection pool to the named database and
// pings it once, so a wrong address or credential fails here instead of at
// the first query. The caller owns the pool: defer Close after a nil error.
// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset (port 5432, schema "vulkan"), Validate rejects what's out of range.
func NewPostgresDatastore(ctx context.Context, user string, host string, database string, cfg *PostgresConnectionConfig) (*PostgresDatastore, error) {
	if user == "" {
		return nil, errors.New("user is required")
	}
	if host == "" {
		return nil, errors.New("host is required")
	}
	if database == "" {
		return nil, errors.New("database is required")
	}

	if cfg == nil {
		cfg = &PostgresConnectionConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	connectionString := fmt.Sprintf("postgres://%s:%s@%s:%s/%s",
		user, cfg.Pass, host, strconv.Itoa(cfg.Port), database,
	)

	poolConfig, err := pgxpool.ParseConfig(connectionString)
	if err != nil {
		return nil, err
	}

	if cfg.MaxConns > 0 {
		poolConfig.MaxConns = int32(cfg.MaxConns)
	}
	if cfg.ConnectTimeout > 0 {
		poolConfig.ConnConfig.ConnectTimeout = cfg.ConnectTimeout
	}
	if cfg.TLSConfig != nil {
		poolConfig.ConnConfig.TLSConfig = cfg.TLSConfig
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, err
	}

	// Sanity check
	if err = pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}

	return &PostgresDatastore{
		Pool:   pool,
		Schema: cfg.Schema,
	}, nil
}

// Close releases the pool. The app that constructed the datastore closes it
// -- producers and consumers borrow it and never do.
func (d *PostgresDatastore) Close() {
	d.Pool.Close()
}
