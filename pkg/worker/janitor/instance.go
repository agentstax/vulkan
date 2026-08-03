package janitor

import (
	"context"
	"errors"

	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/agentstax/vulkan/pkg/worker"
	"github.com/agentstax/vulkan/pkg/worker/controller"
	"github.com/agentstax/vulkan/pkg/worker/janitor/datastore"
)

// JanitorInstance is one claimed live copy of a topic's janitor worker: Run
// sweeps the topic at the row's poll_rate while a heartbeat holds the claim.
type JanitorInstance struct {
	Topic  *topic.Topic
	Config *JanitorConfig
	Logger logger.Logger // copied from Config.Logger at construction

	runner    *controller.InstanceTickRunner
	datastore *datastore.JanitorDatastore
	metadata  *janitorMetadata
}

func newJanitorInstance(janitor *JanitorFactory, current *topic.Topic, claimed *worker.WorkerInstance, metadata *janitorMetadata) (*JanitorInstance, error) {
	if current == nil {
		return nil, errors.New("topic must not be nil")
	}
	if metadata == nil {
		return nil, errors.New("metadata must not be nil")
	}

	runner, err := controller.NewInstanceTickRunner(janitor.workers, claimed, metadata.PollRate, &controller.InstanceTickRunnerConfig{
		InstanceTTL:    janitor.Config.InstanceTTL,
		JitterFraction: janitor.Config.JitterFraction,
		Logger:         logger.With(janitor.Logger, "worker", WorkerJanitor, "topic", current.Id),
		TickRetry:      janitor.Config.SweepRetry,
	})
	if err != nil {
		return nil, err
	}

	return &JanitorInstance{
		Topic:     current,
		Config:    janitor.Config,
		Logger:    janitor.Logger,
		runner:    runner,
		datastore: janitor.datastore,
		metadata:  metadata,
	}, nil
}

// Run sweeps until ctx cancels; a requested stop returns nil. The claimed
// instance releases on the way out however Run exits.
func (i *JanitorInstance) Run(ctx context.Context) error {
	i.Logger.InfoContext(ctx, "janitor starting", "topic", i.Topic.Id, "version", i.Topic.SchemaVersion, "rate", i.metadata.PollRate)

	err := i.runner.Run(ctx, i.sweep)
	if err == nil {
		i.Logger.InfoContext(ctx, "janitor stopped", "topic", i.Topic.Id, "version", i.Topic.SchemaVersion)
	}
	return err
}

// sweep is one janitor pass.
func (i *JanitorInstance) sweep(ctx context.Context) error {
	t := i.Topic
	if err := i.datastore.DropExpiredPartitions(ctx, t.Id, t.PartitionSize, t.RetentionTTL, t.AllowDropPastCommitted, t.DisableDeliveryLog); err != nil {
		return err
	}
	if err := i.datastore.SweepExpiredPartitions(ctx, t.Id, t.PartitionSize, t.RetentionTTL, t.AllowDropPastCommitted, i.metadata.SweepBatchSize, t.DisableDeliveryLog); err != nil {
		return err
	}
	if err := i.datastore.SweepExpiredIdempotencyKeys(ctx, t.Id, t.IdempotencyKeyTTL, i.metadata.SweepBatchSize); err != nil {
		return err
	}
	return i.datastore.SweepExpiredKeyLeases(ctx, t.Id, i.metadata.SweepBatchSize)
}
