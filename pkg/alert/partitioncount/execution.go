package partitioncount

import (
	"context"
	"errors"

	"github.com/agentstax/vulkan/pkg/alert"
	alertcontroller "github.com/agentstax/vulkan/pkg/alert/controller"
	"github.com/agentstax/vulkan/pkg/alert/partitioncount/controller"
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
	alerts     *alertcontroller.AlertController // built per claimed life in consume
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

// consume is one claimed life: the alert controller is built here so every
// claim re-reads the system row's AlertRepeatInterval.
func (e *PartitionCountExecution) consume(ctx context.Context) error {
	system, err := e.definition.systems.GetSystem(ctx)
	if err != nil {
		return err
	}
	if system == nil {
		return migrate.ErrNotRegistered
	}

	registered, err := e.definition.alertProducer.Register(ctx, alert.TopicName, topic.SchemaVersion(1))
	if err != nil {
		return err
	}
	alerts, err := alertcontroller.NewAlertController(registered, e.definition.alertHeads, system.AlertRepeatInterval, e.Logger)
	if err != nil {
		return err
	}
	e.alerts = alerts

	instance, err := e.definition.jobRequestConsumer.Register(ctx, JobName, cron.TopicName, topic.SchemaVersion(1))
	if err != nil {
		return err
	}
	return instance.Consume(ctx, e.evaluateTopics)
}

func (e *PartitionCountExecution) evaluateTopics(ctx context.Context, request *cron.JobRequest) error {
	jobData, err := alert.ToJobData(request.Data)
	if err != nil {
		return err
	}

	topics, err := e.definition.topics.ListTopics(ctx)
	if err != nil {
		return err
	}

	// one topic's failure never skips the others
	var errs error
	for _, listed := range topics {
		owner, err := common.NewTopicOwner(listed.SystemId, listed.Id, listed.Name)
		if err != nil {
			errs = errors.Join(errs, err)
			continue
		}

		found, err := e.definition.controller.Evaluate(ctx, owner, jobData.Threshold)
		if err != nil {
			errs = errors.Join(errs, err)
			continue
		}

		err = e.alerts.Record(ctx, controller.AlertPartitionCount, owner, found)
		if err != nil {
			errs = errors.Join(errs, err)
			continue
		}
	}
	return errs
}
