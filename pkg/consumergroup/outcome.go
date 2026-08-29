package consumergroup

// The two answers a consumerFunc has besides nil and a plain error. The
// runner classifies the returned error: Terminal carries a Permanent
// declaration, Delay carries the duration.

import "time"

// DelayedDelivery is the error value Delay returns; the runner reads the
// duration off it and everything else sees ErrDeliveryDelayed.
type DelayedDelivery struct {
	Delay time.Duration
}

func NewDelayedDelivery(delay time.Duration) *DelayedDelivery {
	return &DelayedDelivery{Delay: delay}
}

func (e *DelayedDelivery) Error() string {
	return e.Unwrap().Error()
}

func (e *DelayedDelivery) Unwrap() error {
	return ErrDeliveryDelayed.With("delay", e.Delay)
}

// Terminal dead-letters this delivery now instead of retrying: cause stays
// reachable through errors.Is/As and renders after the code in last_error.
// Any diagnostic Permanent error classifies the same way; Terminal is the
// spelling for a cause that carries no classification of its own.
func Terminal(cause error) error {
	return ErrDeliveryTerminal.Wrap(cause)
}

// Delay runs this delivery again after delay without counting a failure: the
// row's can_run_after moves out by delay and its delays count goes up by one.
// Zero or less runs it on the next poll.
func Delay(delay time.Duration) error {
	return NewDelayedDelivery(delay)
}
