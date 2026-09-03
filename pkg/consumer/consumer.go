package consumer

import (
	"context"
	"errors"
	"time"

	"github.com/agentstax/vulkan/pkg/alert"
	compactionreadcostcontroller "github.com/agentstax/vulkan/pkg/alert/compactionreadcost/controller"
	partitioncountcontroller "github.com/agentstax/vulkan/pkg/alert/partitioncount/controller"
	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/common/logging"
	"github.com/agentstax/vulkan/pkg/consume"
	consumecontroller "github.com/agentstax/vulkan/pkg/consume/controller"
	"github.com/agentstax/vulkan/pkg/consume/exceptionconsumer"
	"github.com/agentstax/vulkan/pkg/consume/messageconsumer"
	"github.com/agentstax/vulkan/pkg/datastore"
	metricsproducer "github.com/agentstax/vulkan/pkg/metrics/producer"
	"github.com/agentstax/vulkan/pkg/topic"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
	workercontroller "github.com/agentstax/vulkan/pkg/worker/controller"
)

// should be idempotent -- redelivery after a crash or timeout is normal
type ConsumerFunc[Message common.Versioned] func(ctx context.Context, message *Message) error

// Consumer runs a consumer group on one topic. Failed messages retry with
// backoff, and the topic's upkeep (partitions, retention, committed advance) runs
// alongside consumption.
type Consumer struct {
	Config *ConsumerConfig
	Logger logging.Logger

	ds       *datastore.PostgresDatastore
	declared *ConsumerConfig

	topicController *topiccontroller.TopicController
	consumers       *consumecontroller.ConsumeController
	workers         *workercontroller.WorkerController
	evaluators      []alert.Evaluator
}

func NewConsumer(ds *datastore.PostgresDatastore, cfg *ConsumerConfig) (*Consumer, error) {
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}
	if cfg == nil {
		cfg = &ConsumerConfig{}
	}

	// captured before WithDefaults resolves cfg -- the stored document keeps
	// only what the caller set
	declared := cfg.DeepCopy()

	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	cfg.Logger = logging.NewPipelineLogger(cfg.Logger, &logging.PipelineLoggerConfig{Buffer: true, Suppress: true})

	topicController, err := topiccontroller.NewTopicController(ds, &topiccontroller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	consumers, err := consumecontroller.NewConsumeController(ds, &consumecontroller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	workers, err := workercontroller.NewWorkerController(ds, &workercontroller.ControllerConfig{
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

	return &Consumer{
		Config:          cfg,
		Logger:          cfg.Logger,
		ds:              ds,
		declared:        declared,
		topicController: topicController,
		consumers:       consumers,
		workers:         workers,
		evaluators:      []alert.Evaluator{partitionCountController, compactionReadCostController},
	}, nil
}

// Register resolves the named topic and registers the consumer group on it,
// returning an instance that consumes Message from it. Callable many times,
// with a different Message per call -- each call returns an independent
// instance.
// bindings is the group's full set; nil = the whole topic.
// ctx bounds only this call's I/O; the instance's lifetime is Consume's ctx.
func (c *Consumer) Register[Message common.Versioned](ctx context.Context, consumerGroup string, topicName string, bindings []string) (*ConsumerInstance[Message], error) {
	if consumerGroup == "" {
		return nil, errors.New("consumer group is required")
	}
	if topicName == "" {
		return nil, errors.New("topic name is required")
	}

	current, err := c.topicController.Get(ctx, topicName)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, topic.ErrTopicNotFound.With("topic", topicName)
	}
	if err := c.topicController.AssertSchemaSupported(ctx, current.SystemId, current.Id); err != nil {
		return nil, err
	}

	c.logAlerts(ctx, current)

	group, err := c.consumers.RegisterGroup(ctx, current.Id, consumerGroup, c.Config.Start)
	if err != nil {
		return nil, err
	}

	// a consumer-group owner, not the topic's: it reaches up to the topic's
	// janitor and the system's schedule producer, never across to a sibling group
	owner, err := common.NewConsumerGroupOwner(current.SystemId, current.Id, group.Id, group.Name)
	if err != nil {
		return nil, err
	}

	// the registered group config is stored on the group's consumer worker rows
	if err := c.workers.RegisterWorker(ctx, messageconsumer.WorkerMessageConsumer, owner, toMessageConsumerWorkerConfig(c.declared)); err != nil {
		return nil, err
	}
	if err := c.workers.RegisterWorker(ctx, exceptionconsumer.WorkerExceptionConsumer, owner, toExceptionConsumerWorkerConfig(c.declared)); err != nil {
		return nil, err
	}

	declaredAt := time.Now()
	outcome, err := c.consumers.DeclareBindings(ctx, current.Id, group.Id, bindings, declaredAt)
	if err != nil {
		return nil, err
	}
	if outcome == consume.BindingWaiting {
		c.Logger.InfoContext(ctx, "binding declaration waiting -- a live instance still declares a different set; Consume retries until installed",
			"group", group.Name, "patterns", bindings)
	}

	// built per instance -- two instances must never share one event queue
	// or one set of session counters
	instanceMetrics, err := metricsproducer.NewMetricsProducer(c.ds, &metricsproducer.ProducerConfig{
		Logger: c.Config.Logger,
		Retry:  c.Config.Retry,
	})
	if err != nil {
		return nil, err
	}

	return newConsumerInstance[Message](owner, c.ds, instanceMetrics, c.consumers, topicName, common.SchemaVersionOf[Message](), bindings, declaredAt, c.Config)
}
