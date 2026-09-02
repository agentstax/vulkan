package vulkan

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewPostgresPool builds a pool from the parts of a Postgres URL. Its params
// read as the DSN exploded in the order the URL writes it,
// postgres://user:password@host:port/database -- password "" means no
// password, which is real on a trust or peer auth setup.
// It does not dial: NewClient is what pings the pool.
// A caller holding a DSN outright passes pgxpool.New(ctx, dsn) instead and
// hands the result to NewClient, which takes any *pgxpool.Pool.
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
