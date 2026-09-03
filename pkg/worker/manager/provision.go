package manager

import (
	"context"
	"errors"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/worker"
	"github.com/agentstax/vulkan/pkg/worker/controller"
)

// Declare writes the definition as the owner's worker row -- the newest
// declaration wins. Registers run it every time, so a declaration lost to a
// crash heals on the next one.
func (d *ManagerProvisioner) Declare(ctx context.Context, owner *common.Owner) error {
	return d.workers.DeclareWorker(ctx, d.definition, owner)
}

// Provision claims one live instance. declared.Owner is the row's own declared.Owner and the
// instance's reconcile scope -- the deeper the declared.Owner, the shorter the chain.
// nil = declined: the row is suspended, or its instances are already at
// target.
func (d *ManagerProvisioner) Provision(ctx context.Context, declared *worker.WorkerData) (worker.Execution, error) {
	if declared.Owner == nil {
		return nil, errors.New("declared.Owner must not be nil")
	}
	parsed, err := controller.ParseMetadata[managerMetadata](declared.Metadata)
	if err != nil {
		return nil, err
	}
	if err := parsed.Validate(); err != nil {
		return nil, err
	}
	claimed, err := d.workers.RegisterInstance(ctx, declared.Id, declared.Owner, declared.Owner.Kind(), WorkerManager, d.Config.InstanceTTL)
	if err != nil || claimed == nil {
		return nil, err
	}
	return newManagerInstance(d, declared.Owner, claimed, parsed)
}
