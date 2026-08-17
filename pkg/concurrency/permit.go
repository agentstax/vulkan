package concurrency

import "sync/atomic"

// Permit admits one holder at a time: Acquire refuses while held, and the
// returned release frees it. A holder that never releases makes the permit
// one-shot.
type Permit struct {
	held atomic.Bool
}

func NewPermit() (*Permit, error) {
	return &Permit{}, nil
}

// Acquire takes the permit; ok reports whether it was free. Each caller
// words its own refusal error -- the permit doesn't know what it guards.
func (p *Permit) Acquire() (func(), bool) {
	if !p.held.CompareAndSwap(false, true) {
		return nil, false
	}
	return func() { p.held.Store(false) }, true
}
