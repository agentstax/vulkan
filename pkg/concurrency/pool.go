package concurrency

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"golang.org/x/sync/semaphore"
)

// a 'permit' is handed to whoever will own an in-flight unit of work -- the
// holder must always release it when done, ie always 'defer ReleasePermit()'.
type PoolLimiter interface {
	WaitForPermit(ctx context.Context, owner string) error // BLOCKS until one is free
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

func (p *WorkerPoolLimiter) WaitForPermit(ctx context.Context, owner string) error {
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
