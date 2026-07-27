package datastore

import (
	"context"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresDatastore struct {
	Pool *pgxpool.Pool
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewPostgresDatastore(ctx context.Context, cfg *PostgresConnectionConfig) (*PostgresDatastore, error) {
	if cfg == nil {
		cfg = &PostgresConnectionConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	connectionString := fmt.Sprintf("postgres://%s:%s@%s:%s/%s",
		cfg.User, cfg.Pass, cfg.Host, strconv.Itoa(cfg.Port), cfg.Database,
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
		return nil, err
	}

	return &PostgresDatastore{
		Pool: pool,
	}, nil
}

// Close releases the pool. The app that constructed the datastore closes it
// -- producers and consumers borrow it and never do.
func (d *PostgresDatastore) Close() {
	d.Pool.Close()
}
