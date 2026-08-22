package datastore

import (
	"errors"

	"github.com/agentstax/vulkan/pkg/common/diagnostic"
	"github.com/agentstax/vulkan/pkg/migrate"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// errStepLockTimeout reclassifies a lock_timeout expiry (55P03) on the txn
// step path: lock contention is what the step retry exists to ride out, while
// IsTransientPgError alone would stop the run.
var errStepLockTimeout = diagnostic.NewError("VK0053", diagnostic.Transient,
	"could not take a lock needed by the migration step",
	"end the blocking session (pg_stat_activity), then run the migration again")

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

// isLockNotAvailable matches a lock_timeout expiry.
func isLockNotAvailable(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "55P03" // lock_not_available
}
