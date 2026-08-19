package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/worker"
)

// ValidateOwner rejects an owner of the wrong kind for a worker family:
// every Declare starts here. name is only for error text.
func ValidateOwner(owner *common.Owner, ownedBy common.OwnerKind, name string) error {
	if owner == nil {
		return errors.New("owner must not be nil")
	}
	if owner.Kind() != ownedBy {
		return fmt.Errorf("%s workers are %s-owned, got %q owner", name, ownedBy, owner.Kind())
	}
	return nil
}

// RegisterInstance claims one live instance under the worker row. A nil
// instance is a declined claim (target_instances already filled), not an
// error. Callers parse and validate the row's metadata before claiming;
// name is only for error text.
func (c *WorkerController) RegisterInstance(ctx context.Context, workerId int64, owner *common.Owner, ownedBy common.OwnerKind, name string, ttl time.Duration) (*worker.WorkerInstance, error) {
	if workerId <= 0 {
		return nil, fmt.Errorf("workerId must be > 0, got %d", workerId)
	}
	if err := ValidateOwner(owner, ownedBy, name); err != nil {
		return nil, err
	}

	if err := c.AssertSchemaSupported(ctx, owner); err != nil {
		return nil, err
	}

	claimed, err := c.ClaimInstance(ctx, workerId, ttl)
	if err != nil || claimed == nil {
		return nil, err
	}
	return claimed, nil
}
