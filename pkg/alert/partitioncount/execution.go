package partitioncount

import (
	"context"
	"errors"

	"github.com/agentstax/vulkan/pkg/alert"
	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/cron"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/migrate"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/agentstax/vulkan/pkg/worker"
	workercontroller "github.com/agentstax/vulkan/pkg/worker/controller"
)

// PartitionCountExecution consumes the alert's job requests while a heartbeat
// holds the claim.
type PartitionCountExecution struct {
	Owner  *common.Owner
	Logger logger.Logger

	definition *PartitionCountDefinition
	runner     *workercontroller.InstanceRunner
}

func newPartitionCountExecution(definition *PartitionCountDefinition, owner *common.Owner, claimed *worker.WorkerInstance) (*PartitionCountExecution, error) {
	if owner == nil {
		return nil, errors.New("owner must not be nil")
	}

	runner, err := workercontroller.NewInstanceRunner(definition.workers, claimed, &workercontroller.InstanceRunnerConfig{
		InstanceTTL: definition.Config.InstanceTTL,
		Logger:      logger.With(definition.Logger, "worker", JobName, "group", owner.Name),
	})
	if err != nil {
		return nil, err
	}

	return &PartitionCountExecution{
		Owner:      owner,
		Logger:     definition.Logger,
		definition: definition,
		runner:     runner,
	}, nil
}

// Run consumes until ctx cancels; a requested stop returns nil. The claimed
// instance releases on the way out however Run exits.
func (e *PartitionCountExecution) Run(ctx context.Context) error {
	return e.runner.Run(ctx, e.consume)
}

// consume is one claimed life: the publisher is built here so every claim
// re-reads the system row's AlertRepeatInterval.
func (e *PartitionCountExecution) consume(ctx context.Context) error {
	system, err := e.definition.systems.GetSystem(ctx)
	if err != nil {
		return err
	}
	if system == nil {
		return migrate.ErrNotRegistered
	}

	alerts, err := e.definition.alertProducer.Register(ctx, alert.TopicName, topic.SchemaVersion(1))
	if err != nil {
		return err
	}
	publisher, err := alert.NewPublisher(alerts, system.AlertRepeatInterval, e.Logger)
	if err != nil {
		return err
	}
	handler, err := NewHandler(e.definition.ds, publisher, &HandlerConfig{
		Logger: e.definition.Config.Logger,
		Retry:  e.definition.Config.Retry,
	})
	if err != nil {
		return err
	}

	instance, err := e.definition.jobRequestConsumer.Register(ctx, JobName, cron.TopicName, topic.SchemaVersion(1))
	if err != nil {
		return err
	}
	return instance.Consume(ctx, handler.Handle)
}
