package base

// Package base holds the pieces every consumer worker row shares. It
// assembles nothing: consumer.NewConsumer is the assembled group, a
// sub-consumer definition is one worker row.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/consumer/base/controller"
	"github.com/agentstax/vulkan/pkg/datastore"
	metricsproducer "github.com/agentstax/vulkan/pkg/metrics/producer"
	"github.com/agentstax/vulkan/pkg/topic"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
	"github.com/agentstax/vulkan/pkg/worker"
	workercontroller "github.com/agentstax/vulkan/pkg/worker/controller"
)

// BaseDefinition is the half of a consumer worker definition every row
// shares: the worker row's name, the controllers, and the group's
// consumerFunc.
type BaseDefinition[Message any] struct {
	workerName string
	Logger     common.Logger

	workers         *workercontroller.WorkerController
	topics          *topiccontroller.TopicController
	keyLeases       *controller.KeyLeaseController
	abandonedEvents *metricsproducer.MetricsProducer
	consumerFunc    func(ctx context.Context, message *Message) error
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewBaseDefinition[Message any](ds *datastore.PostgresDatastore, workerName string, consumerFunc func(ctx context.Context, message *Message) error, abandonedEvents *metricsproducer.MetricsProducer, cfg *BaseDefinitionConfig) (*BaseDefinition[Message], error) {
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
	if cfg == nil {
		cfg = &BaseDefinitionConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	workers, err := workercontroller.NewWorkerController(ds, &workercontroller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	topics, err := topiccontroller.NewTopicController(ds, &topiccontroller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	keyLeases, err := controller.NewKeyLeaseController(ds, &controller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &BaseDefinition[Message]{
		workerName:      workerName,
		Logger:          cfg.Logger,
		workers:         workers,
		topics:          topics,
		keyLeases:       keyLeases,
		abandonedEvents: abandonedEvents,
		consumerFunc:    consumerFunc,
	}, nil
}

func (d *BaseDefinition[Message]) Name() string {
	return d.workerName
}

// GetTopic resolves the topic a consumer's owner points at; a missing topic
// is an error, not an expected absence -- nothing can consume from it.
func (d *BaseDefinition[Message]) GetTopic(ctx context.Context, topicId int64) (*topic.Topic, error) {
	current, err := d.topics.GetById(ctx, topicId)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, fmt.Errorf("%w: topic %d", topic.ErrTopicNotFound, topicId)
	}
	return current, nil
}

// DeclareWorker creates the group's worker row and writes metadata onto it --
// the newest declaration wins.
// NoInstanceTarget: a consumer's claim gate is the caller asking to consume,
// not a count on the row.
func (d *BaseDefinition[Message]) DeclareWorker(ctx context.Context, owner *common.Owner, metadata any) error {
	if err := workercontroller.ValidateOwner(owner, common.OwnerConsumerGroup, d.workerName); err != nil {
		return err
	}

	return d.workers.RegisterWorker(ctx, d.workerName, owner, &workercontroller.WorkerConfig{
		Metadata:        metadata,
		TargetInstances: worker.NoInstanceTarget,
	})
}

// RegisterInstance claims one live instance under the worker row; a nil
// instance is a declined claim, not an error.
func (d *BaseDefinition[Message]) RegisterInstance(ctx context.Context, workerId int64, owner *common.Owner, instanceTTL time.Duration) (*worker.WorkerInstance, error) {
	return d.workers.RegisterInstance(ctx, workerId, owner, common.OwnerConsumerGroup, d.workerName, instanceTTL)
}
