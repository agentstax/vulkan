package concurrency

import (
	"context"
	"fmt"
	"time"
)

type Queue[WorkType any] interface {
	Cap() int
	WaitForRoom(ctx context.Context, timeout time.Duration, threshold int) (int, error)
	EnQueue(ctx context.Context, work *WorkType) error
	DeQueue(ctx context.Context) (*WorkType, error)
}

// TODO - might want to play around with the idea of a 'broadcast' signal (ie one signal many listeners) to allow many prefetchers, might improve throughput at high ends
type PressureQueue[WorkType any] struct {
	queue          chan *WorkType
	dequeuedSignal chan *WorkType // dequeuedSignal design assumes a singleton with prefetcher
}

func NewPressureQueue[WorkType any](limit int) (*PressureQueue[WorkType], error) {
	if limit < 1 {
		return nil, fmt.Errorf("limit must be >= 1, got %d", limit)
	}

	return &PressureQueue[WorkType]{
		queue: make(chan *WorkType, limit),
		// consumed by prefetcher, limit of 1, should never be blocking
		dequeuedSignal: make(chan *WorkType, 1),
	}, nil
}

// max amount of work the queue can hold
func (q *PressureQueue[WorkType]) Cap() int {
	return cap(q.queue)
}

// blocking with debounce timeout
func (q *PressureQueue[WorkType]) WaitForRoom(ctx context.Context, timeout time.Duration, threshold int) (int, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		if cap(q.queue)-len(q.queue) >= threshold {
			return cap(q.queue) - len(q.queue), nil // full batch return immediately for claiming
		}

		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-timer.C: // debounce timed out -> allow attempt at claiming work but can be 0
			return cap(q.queue) - len(q.queue), nil
		case <-q.dequeuedSignal:
			continue // get back into loop to test and see if cap threshold is met
		}
	}
}

// blocking
func (q *PressureQueue[WorkType]) EnQueue(ctx context.Context, work *WorkType) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case q.queue <- work:
		return nil
	}
}

// blocking
func (q *PressureQueue[WorkType]) DeQueue(ctx context.Context) (*WorkType, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case work := <-q.queue:
		// non-blocking dequeuedSignal.
		// If prefetcher is waiting for this signal -> it will try via WaitForRoom
		// If prefetcher is not waiting -> doesn't matter
		select {
		case q.dequeuedSignal <- work:
		default:
		}

		return work, nil
	}
}
