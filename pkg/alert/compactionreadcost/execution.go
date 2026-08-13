package compactionreadcost

import (
	"context"
	"errors"

	"github.com/agentstax/vulkan/pkg/alert"
	"github.com/agentstax/vulkan/pkg/alert/compactionreadcost/controller"
	alertcontroller "github.com/agentstax/vulkan/pkg/alert/controller"
	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/cron"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/migrate"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/agentstax/vulkan/pkg/worker"
	workercontroller "github.com/agentstax/vulkan/pkg/worker/controller"
)

// CompactionReadCostExecution consumes the alert's job requests while a
// heartbeat holds the claim.
type CompactionReadCostExecution struct {
	Owner  *common.Owner
	Logger logger.Logger

	definition *CompactionReadCostDefinition
	runner     *workercontroller.InstanceRunner
	alerts     *alertcontroller.AlertController // built per claimed life in consume
}

func newCompactionReadCostExecution(definition *CompactionReadCostDefinition, owner *common.Owner, claimed *worker.WorkerInstance) (*CompactionReadCostExecution, error) {
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

	return &CompactionReadCostExecution{
		Owner:      owner,
		Logger:     definition.Logger,
		definition: definition,
		runner:     runner,
	}, nil
}

// Run consumes until ctx cancels; a requested stop returns nil. The claimed
// instance releases on the way out however Run exits.
func (e *CompactionReadCostExecution) Run(ctx context.Context) error {
	return e.runner.Run(ctx, e.consume)
}

// consume is one claimed life: the alert controller is built here so every
// claim re-reads the system row's AlertRepeatInterval.
func (e *CompactionReadCostExecution) consume(ctx context.Context) error {
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
	alerts, err := alertcontroller.NewAlertController(registered, system.AlertRepeatInterval, e.Logger)
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

func (e *CompactionReadCostExecution) evaluateTopics(ctx context.Context, request *cron.JobRequest) error {
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

		err = e.alerts.Record(ctx, controller.AlertCompactionReadCost, owner, found)
		if err != nil {
			errs = errors.Join(errs, err)
			continue
		}
	}
	return errs
}
