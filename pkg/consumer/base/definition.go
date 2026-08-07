package base

import (
	"context"
	"errors"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/consumer/base/controller"
	consumermetrics "github.com/agentstax/vulkan/pkg/consumer/metrics"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/retry"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
	"github.com/agentstax/vulkan/pkg/worker"
	workercontroller "github.com/agentstax/vulkan/pkg/worker/controller"
)

// BaseDefinition is the half of a consumer worker definition every row
// shares: the worker row's name, the controllers, and the group's
// consumerFunc.
type BaseDefinition[Message any] struct {
	workerName string
	Logger     logger.Logger

	workers         *workercontroller.WorkerController
	topics          *topiccontroller.TopicController
	keyLeases       *controller.KeyLeaseController
	abandonedEvents *consumermetrics.MetricEventProducer
	consumerFunc    func(ctx context.Context, message *Message) error
}

func NewBaseDefinition[Message any](ds *datastore.PostgresDatastore, workerName string, consumerFunc func(ctx context.Context, message *Message) error, abandonedEvents *consumermetrics.MetricEventProducer, retryPolicy *retry.Policy, log logger.Logger) (*BaseDefinition[Message], error) {
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}
	if workerName == "" {
		return nil, errors.New("workerName is required")
	}
	if consumerFunc == nil {
		return nil, errors.New("consumerFunc must not be nil")
	}
	if abandonedEvents == nil {
		return nil, errors.New("abandonedEvents producer must not be nil")
	}

	workers, err := workercontroller.NewWorkerController(ds, &workercontroller.ControllerConfig{
		Logger: log,
		Retry:  retryPolicy,
	})
	if err != nil {
		return nil, err
	}
	topics, err := topiccontroller.NewTopicController(ds, &topiccontroller.ControllerConfig{
		Logger: log,
		Retry:  retryPolicy,
	})
	if err != nil {
		return nil, err
	}
	keyLeases, err := controller.NewKeyLeaseController(ds, &controller.ControllerConfig{
		Logger: log,
		Retry:  retryPolicy,
	})
	if err != nil {
		return nil, err
	}

	return &BaseDefinition[Message]{
		workerName:      workerName,
		Logger:          log,
		workers:         workers,
		topics:          topics,
		keyLeases:       keyLeases,
		abandonedEvents: abandonedEvents,
		consumerFunc:    consumerFunc,
	}, nil
}

func (c *BaseDefinition[Message]) Name() string {
	return c.workerName
}

// Declare creates the owner group's worker row; an existing row is left
// untouched, so a declaration lost to a crash heals on the next Consume.
// NoInstanceTarget: a consumer's claim gate is the caller asking to consume,
// not a count on the row.
func (c *BaseDefinition[Message]) Declare(ctx context.Context, owner *common.Owner) error {
	if err := workercontroller.ValidateOwner(owner, common.OwnerConsumerGroup, c.workerName); err != nil {
		return err
	}

	return c.workers.InsertWorker(ctx, c.workerName, owner, &workercontroller.WorkerConfig{
		TargetInstances: worker.NoInstanceTarget,
	})
}

// a nil instance is a declined claim, not an error -- try again later.
func (c *BaseDefinition[Message]) RegisterInstance(ctx context.Context, workerId int64, owner *common.Owner, metadata any, instanceTTL time.Duration) (*worker.WorkerInstance, error) {
	claimed, _, err := workercontroller.RegisterInstance[baseMetadata](ctx, c.workers, workerId, owner, common.OwnerConsumerGroup, c.workerName, metadata, instanceTTL)
	if err != nil || claimed == nil {
		return nil, err
	}
	return claimed, nil
}
