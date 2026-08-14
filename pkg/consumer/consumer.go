package consumer

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/alert"
	compactionreadcostcontroller "github.com/agentstax/vulkan/pkg/alert/compactionreadcost/controller"
	partitioncountcontroller "github.com/agentstax/vulkan/pkg/alert/partitioncount/controller"
	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/consumer/binding"
	consumercontroller "github.com/agentstax/vulkan/pkg/consumer/controller"
	consumermetrics "github.com/agentstax/vulkan/pkg/consumer/metrics"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/topic"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
)

// should be idempotent -- redelivery after a crash or timeout is normal
type ConsumerFunc[Message any] func(ctx context.Context, message *Message) error

// Consumer runs a consumer group on one topic. Failed messages retry with
// backoff, and the topic's upkeep (partitions, retention, waterline) runs
// alongside consumption.
type Consumer[Message any] struct {
	Config *ConsumerConfig
	Logger logger.Logger

	ds *datastore.PostgresDatastore

	topicController *topiccontroller.TopicController
	consumers       *consumercontroller.ConsumerController
	evaluators      []alert.Evaluator
}

func NewConsumer[Message any](ds *datastore.PostgresDatastore, cfg *ConsumerConfig) (*Consumer[Message], error) {
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}

	if cfg == nil {
		cfg = &ConsumerConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	topicController, err := topiccontroller.NewTopicController(ds, &topiccontroller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}
	consumers, err := consumercontroller.NewConsumerController(ds, &consumercontroller.ControllerConfig{
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
	return &Consumer[Message]{
		Config:          cfg,
		Logger:          cfg.Logger,
		ds:              ds,
		topicController: topicController,
		consumers:       consumers,
		evaluators:      []alert.Evaluator{partitionCountController, compactionReadCostController},
	}, nil
}

// Register resolves the named topic and registers the consumer group on it,
// returning an instance ready to Consume. Callable many times -- each call
// returns an independent instance.
// bindings is the group's full set; nil = the whole topic.
// ctx bounds only this call's I/O; the instance's lifetime is Consume's ctx.
func (c *Consumer[Message]) Register(ctx context.Context, consumerGroup string, topicName string, version topic.SchemaVersion, bindings []string) (*ConsumerInstance[Message], error) {
	if consumerGroup == "" {
		return nil, errors.New("consumer group is required")
	}
	if topicName == "" {
		return nil, errors.New("topic name is required")
	}
	if version < 1 {
		return nil, fmt.Errorf("SchemaVersion must be >= 1, got %d", version)
	}

	current, err := c.topicController.GetTopic(ctx, topicName, version)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, fmt.Errorf("%w: topic %q version %d -- register it with MessageAdmin.RegisterTopic first", topic.ErrTopicNotFound, topicName, version)
	}
	if err := c.topicController.AssertSchemaSupported(ctx, current.SystemId, current.Id); err != nil {
		return nil, err
	}

	c.logAlerts(ctx, current)

	group, err := c.consumers.RegisterGroup(ctx, current.Id, consumerGroup)
	if err != nil {
		return nil, err
	}

	// a consumer-group owner, not the topic's: it reaches up to the topic's
	// janitor and the system's cron scheduler, never across to a sibling group
	owner, err := common.NewConsumerGroupOwner(current.SystemId, current.Id, group.Id, group.Name)
	if err != nil {
		return nil, err
	}

	declaredAt := time.Now()
	outcome, err := c.consumers.DeclareBindings(ctx, group.Id, bindings, declaredAt)
	if err != nil {
		return nil, err
	}
	if outcome == binding.DeclarationWaiting {
		c.Logger.InfoContext(ctx, "binding declaration waiting -- a live instance still declares a different set; Consume retries until installed",
			"group", group.Name, "patterns", bindings)
	}

	// built per instance -- two instances must never share one event queue
	abandonedEvents, err := consumermetrics.NewMetricEventProducer(c.ds, &consumermetrics.MetricEventConfig{
		Logger: c.Config.Logger,
		Retry:  c.Config.Retry,
	})
	if err != nil {
		return nil, err
	}

	return NewConsumerInstance[Message](owner, c.ds, abandonedEvents, c.consumers, bindings, declaredAt, c.Config)
}
