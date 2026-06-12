package concurrency

import "errors"

// could consider using struct{} instead of generic WorkType
// generics can be confusing, struct you would need to cast on

var ErrQueueFull = errors.New("queue full")
var ErrQueueEmpty = errors.New("queue empty")

type Queue[WorkType any] interface {
	CanEnQueue() bool
	EnQueue(work WorkType) error

	CanDeQueue() bool
	DeQueue() (WorkType, error)
}

type PressureQueue[WorkType any] struct {
	queue chan WorkType
}

func NewPressureQueue[WorkType any](limit int) (*PressureQueue[WorkType], error) {
	return &PressureQueue[WorkType]{
		queue: make(chan WorkType, limit),
	}, nil
}

func (q *PressureQueue[WorkType]) CanEnQueue() bool {
	if len(q.queue) < cap(q.queue) {
		return true
	}
	return false
}

// non blocking and atomic ie doesn't rely on CanEnQueue
func (q *PressureQueue[WorkType]) EnQueue(work WorkType) error {
	select {
	case q.queue <- work:
		return nil
	default:
		return ErrQueueFull
	}
}

func (q *PressureQueue[WorkType]) CanDeQueue() bool {
	if len(q.queue) > 0 {
		return true
	}
	return false
}

func (q *PressureQueue[WorkType]) DeQueue() (WorkType, error) {
	return <-q.queue, nil
}
