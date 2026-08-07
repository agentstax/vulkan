package datastore

import (
	"crypto/tls"
	"errors"
	"fmt"
	"time"
)

type PostgresConnectionConfig struct {
	User           string
	Pass           string
	Host           string
	Port           int // Default: 5432.
	Database       string
	MaxConns       int           // optional; if > 0 sets the pool's max size. default pgx pool is max(4, numCPU), which caps high worker counts.
	ConnectTimeout time.Duration // optional; mirrors pgconn.Config.ConnectTimeout. Zero means pgx's own default (no timeout).
	TLSConfig      *tls.Config   // optional; mirrors pgconn.Config.TLSConfig. Nil means pgx's own default DSN negotiation (sslmode "prefer": attempt TLS, fall back to plaintext) since this package never sets sslmode itself.
}

// WithDefaults fills Port (5432) -- the one knob that's a protocol constant,
// User/Host/Database never default: a misconfigured deployment must fail loudly
// at Validate, not silently connect to a local database.
func (c *PostgresConnectionConfig) WithDefaults() *PostgresConnectionConfig {
	if c.Port == 0 {
		c.Port = 5432
	}
	return c
}

// Validate runs after WithDefaults -- anything still out of range here was
// set by the caller, not left unset.
func (c *PostgresConnectionConfig) Validate() error {
	if c.User == "" {
		return errors.New("User is required")
	}
	if c.Host == "" {
		return errors.New("Host is required")
	}
	if c.Database == "" {
		return errors.New("Database is required")
	}
	if c.Port <= 0 {
		return fmt.Errorf("Port must be > 0, got %d", c.Port)
	}
	if c.MaxConns < 0 {
		return fmt.Errorf("MaxConns must be >= 0, got %d", c.MaxConns)
	}
	if c.ConnectTimeout < 0 {
		return fmt.Errorf("ConnectTimeout must be >= 0, got %s", c.ConnectTimeout)
	}
	// Pass is deliberately not required -- trust/peer auth setups run with no password
	return nil
}
