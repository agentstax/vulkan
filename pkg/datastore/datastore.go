package datastore

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strconv"

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

// NewPostgresPool builds a pool from the parts of a Postgres URL
func NewPostgresPool(ctx context.Context, user string, password string, host string, database string, cfg *PostgresConnectionConfig) (*pgxpool.Pool, error) {
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

	poolConfig, err := pgxpool.ParseConfig(connectionString(user, password, host, database, cfg.Port))
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

	return pgxpool.NewWithConfig(ctx, poolConfig)
}

// ***************
// *** HELPERS ***
// ***************

// connectionString builds the DSN through net/url so a password holding @ or
// #, and an IPv6 host, survive into it.
func connectionString(user string, password string, host string, database string, port int) string {
	userInfo := url.User(user)
	if password != "" {
		userInfo = url.UserPassword(user, password)
	}

	dsn := url.URL{
		Scheme: "postgres",
		User:   userInfo,
		Host:   net.JoinHostPort(host, strconv.Itoa(port)),
		Path:   "/" + database,
	}
	return dsn.String()
}
