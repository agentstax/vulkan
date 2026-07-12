package retry

import (
	"context"
	"errors"
	"net"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// DatastoreRetry is Retry specialized for Postgres: same backoff/attempt machinery,
// but the error returned from retryableFunc is classified automatically
// instead of every call site wrapping it by hand.
type DatastoreRetry struct {
	*Retry
}

func NewDatastoreRetry(maxRetries int, baseDelay time.Duration, maxDelay time.Duration, exponent int) *DatastoreRetry {
	return &DatastoreRetry{
		Retry: NewRetry(maxRetries, baseDelay, maxDelay, exponent),
	}
}

// Wrap shadows the embedded Retry.Wrap -- same signature, so call sites keep
// writing the real DB call with no manual classification.
func (d *DatastoreRetry) Wrap(ctx context.Context, retryableFunc RetryableFunc) error {
	return d.Retry.Wrap(ctx, func() error {
		return pgClassify(retryableFunc())
	})
}

// pgClassify wraps IsTransientPgError into the RetryableError/PermanentError shape Wrap expects.
func pgClassify(err error) error {
	if err == nil {
		return nil
	}
	if IsTransientPgError(err) {
		return NewRetryableError(err)
	}
	return NewPermanentError(err)
}

// IsTransientPgError reports whether err looks like a transport-level blip
// (never reached the server, or a timeout) rather than a deterministic
// rejection (a *pgconn.PgError, consumer.ErrLeaseLost).
func IsTransientPgError(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	if pgconn.SafeToRetry(err) || pgconn.Timeout(err) {
		return true
	}

	_, ok := errors.AsType[net.Error](err)
	return ok
}
