package retry

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/agentstax/vulkan/pkg/logger"
)

// there are plenty of retry go libs out there however b/c it is relatively basic functionality to implement I'd rather
// create a custom impementation than introduce another dependency which has to be managed and secured

const MIN_DELAY = 0

type RetryableFunc func() error

type Retry struct {
	MaxRetries int
	BaseDelay  time.Duration
	MaxDelay   time.Duration
	Exponent   int
	Logger     logger.Logger
}

func NewRetry(maxRetries int, baseDelay time.Duration, maxDelay time.Duration, exponent int, log logger.Logger) *Retry {
	return &Retry{
		MaxRetries: maxRetries,
		BaseDelay:  baseDelay,
		MaxDelay:   maxDelay,
		Exponent:   exponent,
		Logger:     log,
	}
}

func (r *Retry) Wrap(ctx context.Context, retryableFunc RetryableFunc) error {
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

		// recieved PermanentError -> exit early
		if !IsRetryable(err) {
			return errors.Join(append(retryErrs, err)...)
		}

		retryErrs = append(retryErrs, err)

		// last attempt already spent -- no point sleeping before returning
		if retryCount == r.MaxRetries-1 {
			break
		}

		// calc exponential backoff delay
		delay := r.clamp(time.Duration(float64(r.BaseDelay) * math.Pow(float64(r.Exponent), float64(retryCount))))

		r.Logger.DebugContext(ctx, "retrying after transient error", "attempt", retryCount+1, "max_retries", r.MaxRetries, "delay", delay, "error", err)

		select {
		case <-ctx.Done():
			return errors.Join(append(retryErrs, ctx.Err())...)
		case <-time.After(delay):
			continue
		}
	}

	if len(retryErrs) > 0 {
		r.Logger.WarnContext(ctx, "retry attempts exhausted", "max_retries", r.MaxRetries, "error", errors.Join(retryErrs...))
	}

	return errors.Join(retryErrs...)
}

func (r *Retry) clamp(value time.Duration) time.Duration {
	return max(MIN_DELAY, min(value, r.MaxDelay))
}
