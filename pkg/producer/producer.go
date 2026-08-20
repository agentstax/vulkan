package producer

import (
	"context"
	"errors"
	"fmt"

	"github.com/agentstax/vulkan/pkg/alert"
	compactionreadcostcontroller "github.com/agentstax/vulkan/pkg/alert/compactionreadcost/controller"
	partitioncountcontroller "github.com/agentstax/vulkan/pkg/alert/partitioncount/controller"
	"github.com/agentstax/vulkan/pkg/common"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer/controller"
	"github.com/agentstax/vulkan/pkg/topic"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
)

// ProducerFunc runs inside the append's transaction; the type and its docs
// live with the datastore.
type ProducerFunc[Message any] = controller.ProduceFunc[Message]

type Producer[Message any] struct {
	Config *ProducerConfig
	Logger common.Logger

	controller      *controller.ProducerController[Message]
	topicController *topiccontroller.TopicController
	evaluators      []alert.Evaluator
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewProducer[Message any](ds *iDatastore.PostgresDatastore, cfg *ProducerConfig) (*Producer[Message], error) {
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

	producerController, err := controller.NewProducerController[Message](ds, &controller.ControllerConfig{
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

	return &Producer[Message]{
		Config:          cfg,
		Logger:          cfg.Logger,
		controller:      producerController,
		topicController: topicController,
		evaluators:      []alert.Evaluator{partitionCountController, compactionReadCostController},
	}, nil
}

// Register resolves the named topic against the live topic row and returns an
// instance that produces to it. Callable many times -- each call returns an
// independent instance. ctx bounds only this call's I/O.
func (p *Producer[Message]) Register(ctx context.Context, topicName string, version topic.SchemaVersion) (*ProducerInstance[Message], error) {
	if topicName == "" {
		return nil, errors.New("topic name is required")
	}
	if version < 1 {
		return nil, fmt.Errorf("SchemaVersion must be >= 1, got %d", version)
	}

	current, err := p.topicController.Get(ctx, topicName, version)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, topic.ErrTopicNotFound.With("topic", topicName, "version", version)
	}

	// fail fast if the db's schema is outside the range this build understands
	if err := p.topicController.AssertSchemaSupported(ctx, current.SystemId, current.Id); err != nil {
		return nil, err
	}

	p.logAlerts(ctx, current)

	return NewProducerInstance(current, p.controller, p.Config)
}
