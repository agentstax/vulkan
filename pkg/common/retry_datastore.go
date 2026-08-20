package common

import (
	"context"
	"errors"
	"net"
	"time"

	"github.com/agentstax/vulkan/pkg/common/diagnostic"
	"github.com/agentstax/vulkan/pkg/common/logging"
	"github.com/jackc/pgx/v5/pgconn"
)

type RetryableFunc func() error

// RetryDatastore reruns a datastore call on transient errors, shared by
// producer/consumer/topic: one backoff/attempt machinery, with
// IsTransientDatastoreError as the classification -- errors surface as-is,
// never rewrapped.
type RetryDatastore struct {
	*RetryPolicy
	Logger logging.Logger
}

// policy may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewRetryDatastore(policy *RetryPolicy, log logging.Logger) (*RetryDatastore, error) {
	if log == nil {
		return nil, errors.New("logger must not be nil")
	}
	policy = policy.WithDefaults()
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	return &RetryDatastore{
		RetryPolicy: policy,
		Logger:      log,
	}, nil
}

func (r *RetryDatastore) Wrap(ctx context.Context, retryableFunc RetryableFunc) error {
	var retryErrs []error
	for retryCount := range r.MaxRetries {
		// respect context cancelation
		if ctx.Err() != nil {
			return errors.Join(append(retryErrs, ctx.Err())...)
		}

		err := retryableFunc()

		if err == nil {
			return nil // success -- prior (now-irrelevant) retry errors don't belong in the result
		}

		// permanent -> exit early
		if !IsTransientDatastoreError(err) {
			return errors.Join(append(retryErrs, err)...)
		}

		retryErrs = append(retryErrs, err)

		// last attempt already spent -- no point sleeping before returning
		if retryCount == r.MaxRetries-1 {
			break
		}

		delay := r.CalculateDelay(retryCount)

		r.Logger.DebugContext(ctx, "retrying datastore call", "attempt", retryCount+1, "max_retries", r.MaxRetries, "delay", delay, "error", err)

		select {
		case <-ctx.Done():
			return errors.Join(append(retryErrs, ctx.Err())...)
		case <-time.After(delay):
			continue
		}
	}

	return errors.Join(retryErrs...)
}

// IsTransientDatastoreError is RetryDatastore's classification: recovery
// declared on the error decides; a bare error is judged by IsTransientPgError.
func IsTransientDatastoreError(err error) bool {
	if classified, ok := errors.AsType[*diagnostic.Error](err); ok {
		return classified.Recovery == diagnostic.Transient
	}
	return IsTransientPgError(err)
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
