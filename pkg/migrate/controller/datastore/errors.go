package datastore

import (
	"errors"

	"github.com/agentstax/vulkan/pkg/migrate"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// registrationError maps a missing row or missing table (42P01) to
// migrate.ErrNotRegistered; every other error passes through.
func registrationError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return migrate.ErrNotRegistered
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "42P01" {
		return migrate.ErrNotRegistered
	}
	return err
}
