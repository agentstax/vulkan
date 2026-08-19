package common

import "errors"

// RetryableError wraps an error and indicates it can be retried
type RetryableError struct {
	Err error
}

func NewRetryableError(err error) *RetryableError {
	return &RetryableError{Err: err}
}

func (e *RetryableError) Error() string {
	return e.Err.Error()
}

func (e *RetryableError) Unwrap() error {
	return e.Err
}

// PermanentError wraps an error that should not be retried
type PermanentError struct {
	Err error
}

func NewPermanentError(err error) *PermanentError {
	return &PermanentError{Err: err}
}

func (e *PermanentError) Error() string {
	return e.Err.Error()
}

func (e *PermanentError) Unwrap() error {
	return e.Err
}

// IsRetryable checks if an error should trigger a retry
func IsRetryable(err error) bool {
	if _, ok := errors.AsType[*PermanentError](err); ok {
		return false
	}

	if _, ok := errors.AsType[*RetryableError](err); ok {
		return true
	}

	// Default behavior: don't retry on unknown errors
	return false
}
