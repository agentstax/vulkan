package producer

import (
	"context"
	"errors"

	"github.com/agentstax/vulkan/pkg/alert"
	compactionreadcostcontroller "github.com/agentstax/vulkan/pkg/alert/compactionreadcost/controller"
	partitioncountcontroller "github.com/agentstax/vulkan/pkg/alert/partitioncount/controller"
	workerlivenesscontroller "github.com/agentstax/vulkan/pkg/alert/workerliveness/controller"
	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/common/logging"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/produce/controller"
	"github.com/agentstax/vulkan/pkg/topic"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
)

type Producer struct {
	ds *datastore.PostgresDatastore
}

// NewProducer builds the datastore-only registration object. Register owns
// the config because each call returns an independently configured instance.
func NewProducer(ds *datastore.PostgresDatastore) (*Producer, error) {
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}
	return &Producer{ds: ds}, nil
}

// Register resolves the named topic against the live topic row and returns an
// instance that produces Message to it. Callable many times, with a
// different Message per call -- each call returns an independent instance.
// ctx bounds only this call's I/O.
func (p *Producer) Register[Message common.Versioned](ctx context.Context, topicName string, cfg *ProducerConfig) (*ProducerInstance[Message], error) {
	if topicName == "" {
		return nil, errors.New("topic name is required")
	}
	if cfg == nil {
		cfg = &ProducerConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	cfg.Logger = logging.NewPipelineLogger(cfg.Logger, &logging.PipelineLoggerConfig{Buffer: true, Suppress: true})

	produceController, err := controller.NewProduceController(p.ds, &controller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}
	topicController, err := topiccontroller.NewTopicController(p.ds, &topiccontroller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}
	partitionCountController, err := partitioncountcontroller.NewPartitionCountController(p.ds, &partitioncountcontroller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}
	compactionReadCostController, err := compactionreadcostcontroller.NewCompactionReadCostController(p.ds, &compactionreadcostcontroller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}
	workerLivenessController, err := workerlivenesscontroller.NewWorkerLivenessController(p.ds, &workerlivenesscontroller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}
	evaluators := []alert.Evaluator{partitionCountController, compactionReadCostController, workerLivenessController}

	current, err := topicController.Get(ctx, topicName)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, topic.ErrTopicNotFound.With("topic", topicName)
	}

	// fail fast if the db's schema is outside the range this build understands
	if err := topicController.AssertSchemaSupported(ctx, current.SystemId, current.Id); err != nil {
		return nil, err
	}

	p.logAlerts(ctx, current, cfg.Logger, evaluators)

	return NewProducerInstance[Message](current, produceController, cfg)
}
