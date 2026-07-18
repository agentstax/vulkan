package producer

import "sync"

// workQueue coordinates callers and workers: enqueue one at a time, dequeue
// many at once. An idle queue holds zero workers.
type workQueue[Work any] struct {
	mu      sync.Mutex
	work    []*Work
	workers int
}

func (q *workQueue[Work]) enqueue(work *Work) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.work = append(q.work, work)
}

// needsWorker reports whether the caller should start another worker.
func (q *workQueue[Work]) needsWorker(dequeueMax, workerLimit int) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	// first enqueue always gets one; more ONLY under backlog pressure
	if q.workers == 0 || (q.workers < workerLimit && len(q.work) > q.workers*dequeueMax) {
		q.workers++
		return true
	}
	return false
}

func (q *workQueue[Work]) dequeue(max int) []*Work {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.work) == 0 {
		// emptiness check + exit under the same lock as enqueue
		q.workers--
		return nil
	}
	n := min(len(q.work), max)
	work := q.work[:n:n]
	q.work = q.work[n:]
	return work
}
