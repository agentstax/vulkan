package janitor

import (
	"context"
	"errors"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/common/logging"
	janitorcontroller "github.com/agentstax/vulkan/pkg/consumergroup/janitor/controller"
	"github.com/agentstax/vulkan/pkg/worker"
	"github.com/agentstax/vulkan/pkg/worker/controller"
)

// sweeps superseded waiting binding_log rows at the row's poll_rate while a
// heartbeat holds the claim
type JanitorInstance struct {
	Owner  *common.Owner
	Config *JanitorConfig
	Logger logging.Logger

	runner     *controller.InstanceTickRunner
	controller *janitorcontroller.JanitorController
	metadata   *janitorMetadata
}

func newJanitorInstance(provisioner *JanitorProvisioner, owner *common.Owner, claimed *worker.WorkerInstance, metadata *janitorMetadata) (*JanitorInstance, error) {
	if owner == nil {
		return nil, errors.New("owner must not be nil")
	}
	if metadata == nil {
		return nil, errors.New("metadata must not be nil")
	}

	logger := logging.NewPipelineLogger(provisioner.Logger, &logging.PipelineLoggerConfig{Args: []any{"worker", WorkerConsumerGroupJanitor, "system_id", owner.SystemId}})
	runner, err := controller.NewInstanceTickRunner(provisioner.workers, claimed, metadata.PollRate, &controller.InstanceTickRunnerConfig{
		InstanceTTL:    provisioner.Config.InstanceTTL,
		JitterFraction: provisioner.Config.JitterFraction,
		Logger:         logger,
		TickRetry:      provisioner.Config.SweepRetry,
	})
	if err != nil {
		return nil, err
	}

	return &JanitorInstance{
		Owner:      owner,
		Config:     provisioner.Config,
		Logger:     logger,
		runner:     runner,
		controller: provisioner.controller,
		metadata:   metadata,
	}, nil
}

// Run sweeps until ctx cancels; a requested stop returns nil. The claimed
// instance releases on the way out however Run exits.
func (i *JanitorInstance) Run(ctx context.Context) error {
	i.Logger.InfoContext(ctx, "consumer group janitor starting", "vulkan_version", common.BuildVersion(), "rate", i.metadata.PollRate)

	err := i.runner.Run(ctx, i.sweep)
	if err == nil {
		i.Logger.InfoContext(ctx, "consumer group janitor stopped")
	}
	return err
}

// sweep is one janitor pass.
func (i *JanitorInstance) sweep(ctx context.Context) error {
	swept, err := i.controller.SweepExpiredWaitingDeclarations(ctx, waitingDeclarationTTL, i.metadata.SweepBatchSize)
	if err != nil {
		return err
	}
	if swept > 0 {
		i.Logger.DebugContext(ctx, "waiting declarations swept", "swept_count", swept)
	}
	return nil
}
