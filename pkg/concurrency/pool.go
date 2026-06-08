package concurrency

import (
	"slices"
	"sync"
)

// using concept of a 'permit' as this pass that is handed over to creator of thread in pool
// if you are given a permit -> you are good to create a thread
// that created thread should be the 'owner' of the permit and always release at end of work. really that just means always 'defer ReleasePermit()'

// not using a semaphore as it has no native concept of a 'CanAquire' which is stupid
// not using channel as thread ownership guarentees requires recording and accessing owners from multi threads

type PoolLimiter interface {
	CanAcquirePermit(owner string) bool
	AcquirePermit(owner string) error

	CanReleasePermit(owner string) bool
	ReleasePermit(owner string) error
}

type WorkerPoolLimiter struct {
	mu     sync.RWMutex // slightly read heavy, should re-evaluate if mega concurrency 1000+ becomes a use case
	owners []string
	limit  int
}

func NewWorkerPoolLimiter(limit int) (*WorkerPoolLimiter, error) {
	return &WorkerPoolLimiter{
		owners: []string{},
		limit:  limit,
	}, nil
}

func (p *WorkerPoolLimiter) CanAcquirePermit(owner string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	// assumes single thread ownership
	if slices.Contains(p.owners, owner) {
		return false
	}

	if len(p.owners) < p.limit {
		return true
	}

	return false
}

func (p *WorkerPoolLimiter) AcquirePermit(owner string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.owners = append(p.owners, owner)
	return nil
}

func (p *WorkerPoolLimiter) CanReleasePermit(owner string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if slices.Contains(p.owners, owner) {
		return true
	}

	return false
}

func (p *WorkerPoolLimiter) ReleasePermit(owner string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.owners = slices.DeleteFunc(p.owners, func(currentOwner string) bool {
		return currentOwner == owner
	})
	return nil
}
