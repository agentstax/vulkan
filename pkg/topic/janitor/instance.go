package janitor

import (
	"context"
	"errors"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/common/logging"
	"github.com/agentstax/vulkan/pkg/topic"
	janitorcontroller "github.com/agentstax/vulkan/pkg/topic/janitor/controller"
	"github.com/agentstax/vulkan/pkg/worker"
	"github.com/agentstax/vulkan/pkg/worker/controller"
)

// sweeps the topic at the row's poll_rate while a heartbeat holds the claim
type JanitorInstance struct {
	Topic  *topic.Topic
	Config *JanitorConfig
	Logger logging.Logger

	runner     *controller.InstanceTickRunner
	controller *janitorcontroller.JanitorController
	metadata   *janitorMetadata
}

func newJanitorInstance(janitor *JanitorProvisioner, current *topic.Topic, claimed *worker.WorkerInstance, metadata *janitorMetadata) (*JanitorInstance, error) {
	if current == nil {
		return nil, errors.New("topic must not be nil")
	}
	if metadata == nil {
		return nil, errors.New("metadata must not be nil")
	}

	logger := logging.NewPipelineLogger(janitor.Logger, &logging.PipelineLoggerConfig{Args: []any{"worker", WorkerJanitor, "topic_id", current.Id, "version", current.SchemaVersion}})
	runner, err := controller.NewInstanceTickRunner(janitor.workers, claimed, metadata.PollRate, &controller.InstanceTickRunnerConfig{
		InstanceTTL:    janitor.Config.InstanceTTL,
		JitterFraction: janitor.Config.JitterFraction,
		Logger:         logger,
		TickRetry:      janitor.Config.SweepRetry,
	})
	if err != nil {
		return nil, err
	}

	return &JanitorInstance{
		Topic:      current,
		Config:     janitor.Config,
		Logger:     logger,
		runner:     runner,
		controller: janitor.controller,
		metadata:   metadata,
	}, nil
}

// Run sweeps until ctx cancels; a requested stop returns nil. The claimed
// instance releases on the way out however Run exits.
func (i *JanitorInstance) Run(ctx context.Context) error {
	i.Logger.InfoContext(ctx, "janitor starting", "vulkan_version", common.BuildVersion(), "rate", i.metadata.PollRate)

	err := i.runner.Run(ctx, i.sweep)
	if err == nil {
		i.Logger.InfoContext(ctx, "janitor stopped")
	}
	return err
}

// sweep is one janitor pass.
func (i *JanitorInstance) sweep(ctx context.Context) error {
	current := i.Topic
	if err := i.controller.DropExpiredPartitions(ctx, current.Id, current.PartitionSize, current.RetentionTTL, current.AllowDropPastCommitted, current.DeliveryLogMode); err != nil {
		return err
	}
	if err := i.controller.SweepExpiredPartitions(ctx, current.Id, current.PartitionSize, current.RetentionTTL, current.AllowDropPastCommitted, i.metadata.SweepBatchSize, current.DeliveryLogMode); err != nil {
		return err
	}
	if err := i.controller.SweepExpiredIdempotencyKeys(ctx, current.Id, current.IdempotencyKeyTTL, i.metadata.SweepBatchSize); err != nil {
		return err
	}
	return i.controller.SweepExpiredKeyLeases(ctx, current.Id, i.metadata.SweepBatchSize)
}
