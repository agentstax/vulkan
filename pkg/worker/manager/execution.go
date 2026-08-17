package manager

import (
	"context"
	"errors"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/worker"
	"github.com/agentstax/vulkan/pkg/worker/controller"
	"golang.org/x/sync/errgroup"
)

// reconciles the running set against the worker rows on the owner's chain at
// the row's poll_rate while a heartbeat holds the claim
type ManagerExecution struct {
	Owner  *common.Owner // the reconcile scope
	Config *ManagerConfig
	Logger logger.Logger

	runner       *controller.InstanceTickRunner
	workers      *controller.WorkerController
	provisioners map[string]worker.Provisioner
	pool         *executionPool // built in Run
	metadata     *managerMetadata
}

func newManagerExecution(manager *ManagerDefinition, owner *common.Owner, claimed *worker.WorkerInstance, metadata *managerMetadata) (*ManagerExecution, error) {
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

	return &ManagerExecution{
		Owner:        owner,
		Config:       manager.Config,
		Logger:       manager.Logger,
		runner:       runner,
		workers:      manager.workers,
		provisioners: manager.provisioners,
		metadata:     metadata,
	}, nil
}

// Run reconciles until ctx cancels or a spawned instance fails fatally; a
// requested stop returns nil. The claimed instance releases on the way out
// however Run exits.
func (i *ManagerExecution) Run(ctx context.Context) error {
	i.Logger.InfoContext(ctx, "manager instance starting", "scope", i.Owner.Name, "rate", i.metadata.PollRate)

	// a fatal spawned-instance error cancels runCtx through the group
	group, runCtx := errgroup.WithContext(ctx)
	pool, err := newExecutionPool(i.provisioners, group, i.Logger)
	if err != nil {
		return err
	}
	i.pool = pool

	// run is blocking
	err = i.runner.Run(runCtx, i.refresh)

	// every spawned instance's ctx derives from the pass ctx, so the pool is
	// already stopping -- Wait drains the instances and carries the first
	// fatal error. The claim released first: safe, spawned instances hold
	// their own claims, so their targets still hold.
	if groupErr := group.Wait(); err == nil {
		err = groupErr
	}

	if err == nil {
		i.Logger.InfoContext(ctx, "manager instance stopped")
	}
	return err
}

// refresh is one discovery pass.
func (i *ManagerExecution) refresh(ctx context.Context) error {
	workers, err := i.workers.ListWorkers(ctx, i.Owner)
	if err != nil {
		return err
	}
	if err := i.pool.reconcile(ctx, workers); err != nil {
		return err
	}

	swept, err := i.workers.SweepExpiredInstances(ctx)
	if err != nil {
		return err
	}
	if swept > 0 {
		i.Logger.InfoContext(ctx, "swept expired worker instances", "count", swept)
	}
	return nil
}
