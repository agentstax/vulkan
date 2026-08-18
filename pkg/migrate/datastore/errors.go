package datastore

import (
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ErrNotRegistered means the queried owner has no baseline record -- the system
// or topic was never registered, or migration_log is missing.
var ErrNotRegistered = errors.New("schema not registered -- call Register first")

// registrationError maps a missing row or missing table (42P01) to
// ErrNotRegistered; every other error passes through.
func registrationError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotRegistered
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "42P01" {
		return ErrNotRegistered
	}
	return err
}
