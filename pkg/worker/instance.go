package worker

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// WorkerInstance is one live copy of a worker.
type WorkerInstance struct {
	Id        int64
	WorkerId  int64
	Token     uuid.UUID
	ExpiresAt time.Time
	Attempts  int
}

// ClaimInstance claims one live copy of the worker. nil = declined (already
// at target_instances, target 0, or the worker row is gone).
func (c *WorkerController) ClaimInstance(ctx context.Context, workerId int64, ttl time.Duration) (*WorkerInstance, error) {
	claimed, err := c.datastore.ClaimInstance(ctx, workerId, ttl)
	if err != nil || claimed == nil {
		return nil, err
	}
	return toWorkerInstance(claimed), nil
}

// RenewInstance extends an instance the caller already holds.
func (c *WorkerController) RenewInstance(ctx context.Context, instance *WorkerInstance, ttl time.Duration) error {
	return c.datastore.RenewInstance(ctx, toWorkerInstanceData(instance), ttl)
}

// ReleaseInstance removes the instance row immediately, so on a graceful
// shutdown a replacement claims right away instead of waiting out expires_at.
func (c *WorkerController) ReleaseInstance(ctx context.Context, instance *WorkerInstance) error {
	return c.datastore.ReleaseInstance(ctx, toWorkerInstanceData(instance))
}

// SweepExpiredInstances removes instance rows past expires_at, returning the
// count removed.
func (c *WorkerController) SweepExpiredInstances(ctx context.Context) (int64, error) {
	return c.datastore.SweepExpiredInstances(ctx)
}
