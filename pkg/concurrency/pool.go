package concurrency

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"golang.org/x/sync/semaphore"
)

// using concept of a 'permit' as this pass that is handed over to creator of thread in pool
// if you are given a permit -> you are good to create a thread
// that created thread should be the 'owner' of the permit and always release at end of work. really that just means always 'defer ReleasePermit()'

type PoolLimiter interface {
	AcquirePermit(ctx context.Context, owner string) error
	ReleasePermit(ctx context.Context, owner string) error
}

type WorkerPoolLimiter struct {
	mu        sync.RWMutex // slightly read heavy, should re-evaluate if mega concurrency 1000+ becomes a use case
	owners    []string     // one-to-one owner to permit mapping or 1 gothread per permit
	semaphore *semaphore.Weighted
}

func NewWorkerPoolLimiter(limit int) (*WorkerPoolLimiter, error) {
	if limit < 1 {
		return nil, fmt.Errorf("limit must be >= 1, got %d", limit)
	}

	return &WorkerPoolLimiter{
		owners:    []string{},
		semaphore: semaphore.NewWeighted(int64(limit)),
	}, nil
}

func (p *WorkerPoolLimiter) AcquirePermit(ctx context.Context, owner string) error {
	err := p.semaphore.Acquire(ctx, 1)
	if err != nil {
		return err
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	p.owners = append(p.owners, owner)
	return nil
}

func (p *WorkerPoolLimiter) ReleasePermit(ctx context.Context, owner string) error {
	p.semaphore.Release(1)

	p.mu.Lock()
	defer p.mu.Unlock()

	p.owners = slices.DeleteFunc(p.owners, func(currentOwner string) bool {
		return currentOwner == owner
	})
	return nil
}
