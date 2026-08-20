package common

import (
	"context"
	"errors"
	"net"

	"github.com/jackc/pgx/v5/pgconn"
)

// RetryDatastore is Retry specialized for Postgres, shared by producer/consumer/topic:
// same backoff/attempt machinery, but the error returned from retryableFunc is
// classified automatically instead of every call site wrapping it by hand.
type RetryDatastore struct {
	*Retry
}

// policy may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewRetryDatastore(policy *RetryPolicy, log Logger) (*RetryDatastore, error) {
	r, err := NewRetry(policy, log)
	if err != nil {
		return nil, err
	}
	return &RetryDatastore{
		Retry: r,
	}, nil
}

// Wrap shadows the embedded Retry.Wrap -- same signature, so call sites keep
// writing the real DB call with no manual classification.
func (r *RetryDatastore) Wrap(ctx context.Context, retryableFunc RetryableFunc) error {
	return r.Retry.Wrap(ctx, func() error {
		return classify(retryableFunc())
	})
}

// IsTransientPgError reports whether a retry is safe -- never a
// deterministic rejection (a business-logic *pgconn.PgError, ErrLeaseLost).
func IsTransientPgError(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
		switch pgErr.Code {
		// deadlock / serialization_failure -- whole txn provably rolled back.
		case "40P01", "40001":
			return true

		// never sent anything -- nothing could have landed.
		case "08001", "08003", "53300":
			return true

		// query_canceled -- only external cancels reach here; ours are
		// already filtered above. Aborts cleanly.
		case "57014":
			return true

		// connection died after a statement may have shipped, so the outcome
		// is genuinely ambiguous -- every RetryDatastore.Wrap call site is
		// audited for this; an ungated write added to one reopens it.
		case "08000", "08006", "08007", "40003":
			return true

		// same ambiguity, caused by an admin command or restart instead.
		case "57P01", "57P02", "57P03", "57P05":
			return true
		}
		return false
	}

	if pgconn.SafeToRetry(err) || pgconn.Timeout(err) {
		return true
	}

	_, ok := errors.AsType[net.Error](err)
	return ok
}

// ***************
// *** HELPERS ***
// ***************

// classify wraps IsTransientPgError into the RetryableError/PermanentError shape
func classify(err error) error {
	if err == nil {
		return nil
	}

	// an *Error already carries its recovery -- passes through unwrapped
	if _, ok := errors.AsType[*Error](err); ok {
		return err
	}

	if _, ok := errors.AsType[*RetryableError](err); ok {
		return err
	}
	if _, ok := errors.AsType[*PermanentError](err); ok {
		return err
	}
	if IsTransientPgError(err) {
		return NewRetryableError(err)
	}

	// dont retry if not classify-able
	return NewPermanentError(err)
}
