package manager

import (
	"context"
	"errors"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/worker"
	"github.com/agentstax/vulkan/pkg/worker/controller"
)

// ManagerInstance is one claimed live copy of an owner's manager worker: Run
// reconciles the running set against the worker rows on the owner's chain at
// the row's poll_rate while a heartbeat holds the claim.
type ManagerInstance struct {
	Owner  *common.Owner // the reconcile scope
	Config *ManagerConfig
	Logger logger.Logger // copied from Config.Logger at construction

	runner   *controller.InstanceTickRunner
	workers  *controller.WorkerController
	pool     *instancePool
	metadata *managerMetadata
}

func newManagerInstance(manager *ManagerFactory, owner *common.Owner, claimed *worker.WorkerInstance, metadata *managerMetadata) (*ManagerInstance, error) {
	if metadata == nil {
		return nil, errors.New("metadata must not be nil")
	}

	runner, err := controller.NewInstanceTickRunner(manager.workers, claimed, metadata.PollRate, &controller.InstanceTickRunnerConfig{
		InstanceTTL:    manager.Config.InstanceTTL,
		JitterFraction: manager.Config.JitterFraction,
		Logger:         logger.With(manager.Logger, "worker", WorkerManager, "scope", owner.Name),
		TickRetry:      manager.Config.RefreshRetry,
	})
	if err != nil {
		return nil, err
	}

	return &ManagerInstance{
		Owner:    owner,
		Config:   manager.Config,
		Logger:   manager.Logger,
		runner:   runner,
		workers:  manager.workers,
		pool:     newInstancePool(manager.Logger, manager.factories),
		metadata: metadata,
	}, nil
}

// Run reconciles until ctx cancels; a requested stop returns nil. The claimed
// instance releases on the way out however Run exits.
func (i *ManagerInstance) Run(ctx context.Context) error {
	i.Logger.InfoContext(ctx, "manager instance starting", "scope", i.Owner.Name, "rate", i.metadata.PollRate)

	err := i.runner.Run(ctx, i.refresh)

	// every spawned instance's ctx derives from the pass ctx, so the pool is
	// already stopping -- wait for the instances to drain. The claim released
	// first: safe, spawned instances hold their own claims, so their targets
	// still hold.
	i.pool.wait()

	if err == nil {
		i.Logger.InfoContext(ctx, "manager instance stopped")
	}
	return err
}

// refresh is one discovery pass.
func (i *ManagerInstance) refresh(ctx context.Context) error {
	workers, err := i.workers.ListWorkers(ctx, i.Owner)
	if err != nil {
		return err
	}
	i.pool.reconcile(ctx, workers)

	swept, err := i.workers.SweepExpiredInstances(ctx)
	if err != nil {
		return err
	}
	if swept > 0 {
		i.Logger.InfoContext(ctx, "swept expired worker instances", "count", swept)
	}
	return nil
}
