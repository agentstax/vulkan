package datastore

import (
	"crypto/tls"
	"fmt"
	"regexp"
	"time"
)

const DefaultSchema = "vulkan"

// schemaName is the identifier Postgres accepts unquoted and lowercased
var schemaNamePattern = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

type PostgresConnectionConfig struct {
	Schema         string        // The postgres namespace holding every table. Default: "vulkan".
	Pass           string        // optional; Postgres trust/peer auth setups run with no password.
	Port           int           // Default: 5432.
	MaxConns       int           // optional; if > 0 sets the pool's max size. default pgx pool is max(4, numCPU), which caps high worker counts.
	ConnectTimeout time.Duration // optional; mirrors pgconn.Config.ConnectTimeout. Zero means pgx's own default (no timeout).
	TLSConfig      *tls.Config   // optional; mirrors pgconn.Config.TLSConfig. Nil means pgx's own default DSN negotiation (sslmode "prefer": attempt TLS, fall back to plaintext) since this package never sets sslmode itself.
}

// WithDefaults fills Schema (DefaultSchema) and Port (5432) -- the one knob
// that's a protocol constant.
func (c *PostgresConnectionConfig) WithDefaults() *PostgresConnectionConfig {
	if c.Schema == "" {
		c.Schema = DefaultSchema
	}
	if c.Port == 0 {
		c.Port = 5432
	}
	return c
}

// Validate runs after WithDefaults -- anything still out of range here was
// set by the caller, not left unset.
func (c *PostgresConnectionConfig) Validate() error {
	if !schemaNamePattern.MatchString(c.Schema) {
		return fmt.Errorf("Schema must be a lowercase unquoted identifier, got %q", c.Schema)
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
