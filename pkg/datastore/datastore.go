package datastore

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresDatastore struct {
	Pool *pgxpool.Pool

	// Schema is the namespace every vulkan table lives in
	Schema string
}

// NewPostgresDatastore wraps a pool you built and pings it once, so a wrong
// address or credential fails here instead of at the first query.
// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset (schema "vulkan"), Validate rejects what's out of range.
func NewPostgresDatastore(ctx context.Context, pool *pgxpool.Pool, cfg *PostgresDatastoreConfig) (*PostgresDatastore, error) {
	if pool == nil {
		return nil, errors.New("pool must not be nil")
	}

	if cfg == nil {
		cfg = &PostgresDatastoreConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	if err := pool.Ping(ctx); err != nil {
		return nil, err
	}

	return &PostgresDatastore{
		Pool:   pool,
		Schema: cfg.Schema,
	}, nil
}
