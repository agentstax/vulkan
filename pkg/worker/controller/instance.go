package controller

import (
	"context"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/worker"
	"github.com/google/uuid"
)

// validInstanceInputs rejects values no worker_instance row can ever match --
// without it a zero id/token reads as ErrInstanceLost and a zero ttl claims a
// row that's already expired.
func validInstanceInputs(instanceId int64, token uuid.UUID) error {
	if instanceId <= 0 {
		return fmt.Errorf("instanceId must be > 0, got %d", instanceId)
	}
	if token == uuid.Nil {
		return fmt.Errorf("token is required")
	}
	return nil
}

// ClaimInstance claims one live copy of the worker. nil = declined (already
// at target_instances, target 0, or the worker row is gone).
func (c *WorkerController) ClaimInstance(ctx context.Context, workerId int64, ttl time.Duration) (*worker.WorkerInstance, error) {
	if workerId <= 0 {
		return nil, fmt.Errorf("workerId must be > 0, got %d", workerId)
	}
	if ttl <= 0 {
		return nil, fmt.Errorf("ttl must be > 0, got %v", ttl)
	}

	claimed, err := c.datastore.ClaimInstance(ctx, workerId, ttl)
	if err != nil || claimed == nil {
		return nil, err
	}
	return toWorkerInstance(claimed), nil
}

// RenewInstance extends an instance the caller already holds.
func (c *WorkerController) RenewInstance(ctx context.Context, instanceId int64, token uuid.UUID, ttl time.Duration) error {
	if err := validInstanceInputs(instanceId, token); err != nil {
		return err
	}
	if ttl <= 0 {
		return fmt.Errorf("ttl must be > 0, got %v", ttl)
	}
	return c.datastore.RenewInstance(ctx, instanceId, toTokenData(token), ttl)
}

// RecordInstanceSuccess resets the instance's consecutive-failure count.
func (c *WorkerController) RecordInstanceSuccess(ctx context.Context, instanceId int64, token uuid.UUID) error {
	if err := validInstanceInputs(instanceId, token); err != nil {
		return err
	}
	return c.datastore.RecordInstanceSuccess(ctx, instanceId, toTokenData(token))
}

// RecordInstanceFailure adds one to the instance's consecutive-failure count,
// returning the new count.
func (c *WorkerController) RecordInstanceFailure(ctx context.Context, instanceId int64, token uuid.UUID) (int, error) {
	if err := validInstanceInputs(instanceId, token); err != nil {
		return 0, err
	}
	return c.datastore.RecordInstanceFailure(ctx, instanceId, toTokenData(token))
}

// ReleaseInstance removes the instance row immediately, so on a graceful
// shutdown a replacement claims right away instead of waiting out expires_at.
func (c *WorkerController) ReleaseInstance(ctx context.Context, instanceId int64, token uuid.UUID) error {
	if err := validInstanceInputs(instanceId, token); err != nil {
		return err
	}
	return c.datastore.ReleaseInstance(ctx, instanceId, toTokenData(token))
}

// SweepExpiredInstances removes instance rows past expires_at, returning the
// count removed.
func (c *WorkerController) SweepExpiredInstances(ctx context.Context) (int64, error) {
	return c.datastore.SweepExpiredInstances(ctx)
}
