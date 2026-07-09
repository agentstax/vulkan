package datastore

import "errors"

type PostgresConnectionConfig struct {
	User     string
	Pass     string
	Host     string
	Port     int
	Database string
	MaxConns int // optional; if > 0 sets pool_max_conns. default pgx pool is max(4, numCPU), which caps high worker counts.
}

func (c *PostgresConnectionConfig) Validate() error {
	if c.User == "" {
		return errors.New("user is required")
	}
	if c.Host == "" {
		return errors.New("host is required")
	}
	if c.Database == "" {
		return errors.New("database is required")
	}
	if c.Port <= 0 {
		return errors.New("port must be positive")
	}
	if c.MaxConns < 0 {
		return errors.New("max conns must not be negative")
	}
	// Pass is deliberately not required -- trust/peer auth setups run with no password
	return nil
}
