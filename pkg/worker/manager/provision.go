package manager

import (
	"context"
	"errors"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/worker"
	"github.com/agentstax/vulkan/pkg/worker/controller"
)

// Declare creates owner's manager worker row and writes the default config
// onto it -- the newest declaration wins. Registers run it every time, so a
// declaration lost to a crash heals on the next one. Any owner kind declares
// one, and no instance target -- every process reconciling owner's chain
// claims its own.
func (m *ManagerDefinition) Declare(ctx context.Context, owner *common.Owner) error {
	if owner == nil {
		return errors.New("owner must not be nil")
	}

	return m.workers.RegisterWorker(ctx, WorkerManager, owner, &controller.WorkerConfig{
		Metadata:        defaultManagerMetadata(),
		TargetInstances: worker.NoInstanceTarget,
	})
}

// Provision claims one live instance. owner is the row's own owner and the
// instance's reconcile scope -- the deeper the owner, the shorter the chain.
// nil = declined, which for a manager row means target_instances was set away
// from worker.NoInstanceTarget.
func (m *ManagerDefinition) Provision(ctx context.Context, workerId int64, owner *common.Owner, metadata any) (worker.Execution, error) {
	if owner == nil {
		return nil, errors.New("owner must not be nil")
	}
	parsed, err := controller.ParseMetadata[managerMetadata](metadata)
	if err != nil {
		return nil, err
	}
	if err := parsed.Validate(); err != nil {
		return nil, err
	}
	claimed, err := m.workers.RegisterInstance(ctx, workerId, owner, owner.Kind(), WorkerManager, m.Config.InstanceTTL)
	if err != nil || claimed == nil {
		return nil, err
	}
	return newManagerExecution(m, owner, claimed, parsed)
}
