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

// pgClassify is default-deny: only errors that look like a genuine
// transport-level blip (never reached the server, or a timeout) are
// retryable. Everything else -- a *pgconn.PgError (the server rejected the
// query outright, deterministic), context cancellation, or any app-level
// sentinel this package has no knowledge of (e.g. consumer.ErrLeaseLost) --
// defaults to permanent.
func pgClassify(err error) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return NewPermanentError(err)
	}

	if pgconn.SafeToRetry(err) || pgconn.Timeout(err) {
		return NewRetryableError(err)
	}

	if _, ok := errors.AsType[net.Error](err); ok {
		return NewRetryableError(err)
	}

	return NewPermanentError(err)
}
