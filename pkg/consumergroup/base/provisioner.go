package base

// Package base holds the pieces every consumer worker row shares. It
// assembles nothing: consumer.NewConsumer is the assembled group, a
// sub-consumer provisioner runs one worker row.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/common/logging"
	"github.com/agentstax/vulkan/pkg/consumergroup/base/controller"
	"github.com/agentstax/vulkan/pkg/datastore"
	metricsproducer "github.com/agentstax/vulkan/pkg/metrics/producer"
	"github.com/agentstax/vulkan/pkg/topic"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
	"github.com/agentstax/vulkan/pkg/worker"
	workercontroller "github.com/agentstax/vulkan/pkg/worker/controller"
)

// BaseProvisioner is the half of a consumer worker kind every row shares:
// the row's definition, the controllers, and the group's consumerFunc.
type BaseProvisioner[Message topic.Versioned] struct {
	definition *worker.Definition
	Logger     logging.Logger

	workers      *workercontroller.WorkerController
	topics       *topiccontroller.TopicController
	keyLeases    *controller.KeyLeaseController
	metrics      *metricsproducer.MetricsProducer
	consumerFunc func(ctx context.Context, message *Message) error

	// the version the group's Message type declares; the claim reads only rows at it
	schemaVersion int
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewBaseProvisioner[Message topic.Versioned](ds *datastore.PostgresDatastore, definition *worker.Definition, consumerFunc func(ctx context.Context, message *Message) error, schemaVersion int, metrics *metricsproducer.MetricsProducer, cfg *BaseProvisionerConfig) (*BaseProvisioner[Message], error) {
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}
	if definition == nil {
		return nil, errors.New("definition must not be nil")
	}
	if consumerFunc == nil {
		return nil, errors.New("consumerFunc must not be nil")
	}
	if schemaVersion < 1 {
		return nil, fmt.Errorf("schemaVersion must be >= 1, got %d", schemaVersion)
	}
	if metrics == nil {
		return nil, errors.New("metrics must not be nil")
	}
	if cfg == nil {
		cfg = &BaseProvisionerConfig{}
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

	// NoInstanceTarget on every consumer row: a consumer's claim gate is the
	// caller asking to consume, not a count on the row.
	definition.TargetInstances = worker.NoInstanceTarget

	return &BaseProvisioner[Message]{
		definition:    definition,
		Logger:        cfg.Logger,
		workers:       workers,
		topics:        topics,
		keyLeases:     keyLeases,
		metrics:       metrics,
		consumerFunc:  consumerFunc,
		schemaVersion: schemaVersion,
	}, nil
}

func (d *BaseProvisioner[Message]) Definition() *worker.Definition {
	return d.definition
}

// Declare writes the definition as the owner group's worker row -- the
// newest declaration wins.
func (d *BaseProvisioner[Message]) Declare(ctx context.Context, owner *common.Owner) error {
	return d.workers.DeclareWorker(ctx, d.definition, owner)
}

// GetTopic resolves the topic a consumer's owner points at; a missing topic
// is an error, not an expected absence -- nothing can consume from it.
func (d *BaseProvisioner[Message]) GetTopic(ctx context.Context, topicId int64) (*topic.TopicData, error) {
	current, err := d.topics.GetById(ctx, topicId)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, topic.ErrTopicNotFound.With("topic_id", topicId)
	}
	return current, nil
}

// RegisterInstance claims one live instance under the worker row; a nil
// instance is a declined claim, not an error.
func (d *BaseProvisioner[Message]) RegisterInstance(ctx context.Context, workerId int64, owner *common.Owner, instanceTTL time.Duration) (*worker.WorkerInstance, error) {
	return d.workers.RegisterInstance(ctx, workerId, owner, d.definition.OwnerKind, d.definition.Name, instanceTTL)
}
