package consumer

import (
	"context"
	"errors"
	"fmt"

	"github.com/agentstax/vulkan/pkg/common"
	consumercontroller "github.com/agentstax/vulkan/pkg/consumer/controller"
	consumermetrics "github.com/agentstax/vulkan/pkg/consumer/metrics"
	"github.com/agentstax/vulkan/pkg/datastore"
	vulkanerrors "github.com/agentstax/vulkan/pkg/errors"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/maintain"
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

	consumerGroup string
	topicName     string
	version       topic.SchemaVersion
	ds            *datastore.PostgresDatastore

	topicController *topiccontroller.TopicController
	maintenance     *maintain.MaintenanceDatastore
	consumers       *consumercontroller.ConsumerController
	abandonedEvents *consumermetrics.MetricEventProducer
}

func NewConsumer[Message any](consumerGroup string, topicName string, version topic.SchemaVersion, ds *datastore.PostgresDatastore, cfg *ConsumerConfig) (*Consumer[Message], error) {
	if topicName == "" {
		return nil, errors.New("topic name is required")
	}
	if version < 1 {
		return nil, fmt.Errorf("SchemaVersion must be >= 1, got %d", version)
	}
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
	maintenance, err := maintain.NewMaintenanceDatastore(ds, &maintain.MaintenanceDatastoreConfig{
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
	abandonedEvents, err := consumermetrics.NewMetricEventProducer(ds, &consumermetrics.MetricEventConfig{
		DisableGracefulShutdown: cfg.DisableGracefulShutdown,
		Logger:                  cfg.Logger,
		Retry:                   cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &Consumer[Message]{
		Config:          cfg,
		Logger:          cfg.Logger,
		consumerGroup:   consumerGroup,
		topicName:       topicName,
		version:         version,
		ds:              ds,
		topicController: topicController,
		maintenance:     maintenance,
		consumers:       consumers,
		abandonedEvents: abandonedEvents,
	}, nil
}

// ctx is the instance's lifetime: cancel it to wind the instance down. It
// must be cancellable, unless ConsumerConfig.DisableGracefulShutdown declares
// otherwise.
func (c *Consumer[Message]) Register(ctx context.Context) (*ConsumerInstance[Message], error) {
	// Done() == nil -> Background/TODO -> no cancel can ever arrive, so the
	// shutdown phase would silently not exist
	if ctx.Done() == nil && !c.Config.DisableGracefulShutdown {
		return nil, fmt.Errorf("%w: consumer group %q on topic %q\n%s", vulkanerrors.ErrLifecycleContextNotCancellable, c.consumerGroup, c.topicName, lifecycleContextHelp)
	}

	current, err := c.topicController.GetTopic(ctx, c.topicName, c.version)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, fmt.Errorf("%w: topic %q version %d -- register it with MessageAdmin.RegisterTopic first", topic.ErrTopicNotFound, c.topicName, c.version)
	}
	if err := c.topicController.AssertSchemaSupported(ctx, current.SystemId, current.Id); err != nil {
		return nil, err
	}

	// cold-start guarantee: the next partition exists before the janitor's
	// first tick
	if err := c.maintenance.EnsureNextPartition(ctx, current.Id, current.PartitionSize); err != nil {
		return nil, err
	}

	group, err := c.consumers.RegisterGroup(ctx, current.Id, c.consumerGroup)
	if err != nil {
		return nil, err
	}

	// a consumer-group owner, not the topic's: it reaches up to the topic's
	// janitor and the system's cron scheduler, never across to a sibling group
	owner, err := common.NewConsumerGroupOwner(current.SystemId, current.Id, group.Id, group.Name)
	if err != nil {
		return nil, err
	}

	// Register starts a goroutine draining the abandoned-event queue, and this
	// ctx bounds it. A worker claim would be the wrong lifetime: the events are
	// produced as a consumer shuts down, after its claim is already gone
	if err := c.abandonedEvents.Register(ctx); err != nil {
		return nil, err
	}

	return NewConsumerInstance[Message](owner, c.ds, c.abandonedEvents, ctx, c.Config)
}
