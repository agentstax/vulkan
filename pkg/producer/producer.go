package producer

import (
	"context"
	"errors"

	"github.com/agentstax/vulkan/pkg/alert"
	compactionreadcostcontroller "github.com/agentstax/vulkan/pkg/alert/compactionreadcost/controller"
	partitioncountcontroller "github.com/agentstax/vulkan/pkg/alert/partitioncount/controller"
	workerlivenesscontroller "github.com/agentstax/vulkan/pkg/alert/workerliveness/controller"
	"github.com/agentstax/vulkan/pkg/common/logging"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer/controller"
	"github.com/agentstax/vulkan/pkg/topic"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
)

// ProducerFunc runs inside the append's transaction; the type and its docs
// live with the datastore.
type ProducerFunc[Message topic.Versioned] = controller.ProduceFunc[Message]

type Producer struct {
	Config *ProducerConfig
	Logger logging.Logger

	controller      *controller.ProducerController
	topicController *topiccontroller.TopicController
	evaluators      []alert.Evaluator
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewProducer(ds *iDatastore.PostgresDatastore, cfg *ProducerConfig) (*Producer, error) {
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}
	if cfg == nil {
		cfg = &ProducerConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	cfg.Logger = logging.NewPipelineLogger(cfg.Logger, &logging.PipelineLoggerConfig{Buffer: true, Suppress: true})

	producerController, err := controller.NewProducerController(ds, &controller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	topicController, err := topiccontroller.NewTopicController(ds, &topiccontroller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	partitionCountController, err := partitioncountcontroller.NewPartitionCountController(ds, &partitioncountcontroller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}
	compactionReadCostController, err := compactionreadcostcontroller.NewCompactionReadCostController(ds, &compactionreadcostcontroller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	workerLivenessController, err := workerlivenesscontroller.NewWorkerLivenessController(ds, &workerlivenesscontroller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &Producer{
		Config:          cfg,
		Logger:          cfg.Logger,
		controller:      producerController,
		topicController: topicController,
		evaluators:      []alert.Evaluator{partitionCountController, compactionReadCostController, workerLivenessController},
	}, nil
}

// Register resolves the named topic against the live topic row and returns an
// instance that produces Message to it. Callable many times, with a
// different Message per call -- each call returns an independent instance.
// ctx bounds only this call's I/O.
func (p *Producer) Register[Message topic.Versioned](ctx context.Context, topicName string) (*ProducerInstance[Message], error) {
	if topicName == "" {
		return nil, errors.New("topic name is required")
	}

	current, err := p.topicController.Get(ctx, topicName)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, topic.ErrTopicNotFound.With("topic", topicName)
	}

	// fail fast if the db's schema is outside the range this build understands
	if err := p.topicController.AssertSchemaSupported(ctx, current.SystemId, current.Id); err != nil {
		return nil, err
	}

	p.logAlerts(ctx, current)

	return NewProducerInstance[Message](current, p.controller, p.Config)
}
