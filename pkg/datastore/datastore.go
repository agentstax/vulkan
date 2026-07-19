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
	if cfg.MaxConns > 0 {
		connectionString += fmt.Sprintf("?pool_max_conns=%d", cfg.MaxConns)
	}

	pool, err := pgxpool.New(ctx, connectionString)
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

func (d *PostgresDatastore) Shutdown() error {
	d.Pool.Close()
	return nil
}
