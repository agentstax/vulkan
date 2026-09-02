package datastore

import (
	"fmt"
	"regexp"
)

const DefaultSchema = "vulkan"

// schemaName is the identifier Postgres accepts unquoted and lowercased
var schemaNamePattern = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

type PostgresDatastoreConfig struct {
	Schema string // The postgres namespace holding every table. Default: "vulkan".
}

// WithDefaults fills Schema (DefaultSchema).
func (c *PostgresDatastoreConfig) WithDefaults() *PostgresDatastoreConfig {
	if c.Schema == "" {
		c.Schema = DefaultSchema
	}
	return c
}

// Validate runs after WithDefaults -- anything still out of range here was
// set by the caller, not left unset.
func (c *PostgresDatastoreConfig) Validate() error {
	if !schemaNamePattern.MatchString(c.Schema) {
		return fmt.Errorf("Schema must be a lowercase unquoted identifier, got %q", c.Schema)
	}
	return nil
}
