package retry

import (
	"context"
	"errors"
	"math"
	"time"
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
}

func NewRetry(maxRetries int, baseDelay time.Duration, maxDelay time.Duration, exponent int) *Retry {
	return &Retry{
		MaxRetries: maxRetries,
		BaseDelay:  baseDelay,
		MaxDelay:   maxDelay,
		Exponent:   exponent,
	}
}

// TODO - consider adding logging for users to understand retries are happening
func (r *Retry) Wrap(ctx context.Context, retryableFunc RetryableFunc) error {
	var retryErrs []error
	for retryCount := range r.MaxRetries {
		// respect context cancelation
		if ctx.Err() != nil {
			return errors.Join(append(retryErrs, ctx.Err())...)
		}

		err := retryableFunc()

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

		select {
		case <-ctx.Done():
			return errors.Join(append(retryErrs, ctx.Err())...)
		case <-time.After(delay):
			continue
		}
	}

	return errors.Join(retryErrs...)
}

func (r *Retry) clamp(value time.Duration) time.Duration {
	return max(MIN_DELAY, min(value, r.MaxDelay))
}
