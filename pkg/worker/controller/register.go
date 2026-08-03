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
// every factory Seed starts here. name is only for error text.
func ValidateOwner(owner *common.Owner, ownedBy common.OwnerKind, name string) error {
	if owner == nil {
		return errors.New("owner must not be nil")
	}
	if owner.Kind() != ownedBy {
		return fmt.Errorf("%s workers are %s-owned, got %q owner", name, ownedBy, owner.Kind())
	}
	return nil
}

// RegisterInstance is the shared body of every factory Register: validate
// the inputs, parse the row's metadata, assert the owner's schema, then
// claim one live instance. A nil instance means declined -- target_instances
// already filled -- which is not an error; the manager retries next
// reconcile. name is only for error text.
func RegisterInstance[Metadata any, Pointer interface {
	*Metadata
	Validate() error
}](ctx context.Context, workers *WorkerController, workerId int64, owner *common.Owner, ownedBy common.OwnerKind, name string, metadata any, ttl time.Duration) (*worker.WorkerInstance, *Metadata, error) {
	if workerId <= 0 {
		return nil, nil, fmt.Errorf("workerId must be > 0, got %d", workerId)
	}
	if err := ValidateOwner(owner, ownedBy, name); err != nil {
		return nil, nil, err
	}

	parsed, err := ParseMetadata[Metadata, Pointer](metadata)
	if err != nil {
		return nil, nil, err
	}

	if err := workers.AssertSchemaSupported(ctx, owner); err != nil {
		return nil, nil, err
	}

	claimed, err := workers.ClaimInstance(ctx, workerId, ttl)
	if err != nil || claimed == nil {
		return nil, nil, err
	}
	return claimed, parsed, nil
}
