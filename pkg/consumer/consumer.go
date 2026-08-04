package consumer

import (
	"context"
	"errors"
	"fmt"

	"github.com/agentstax/vulkan/pkg/common"
	consumermetrics "github.com/agentstax/vulkan/pkg/consumer/metrics"
	"github.com/agentstax/vulkan/pkg/datastore"
	vulkanerrors "github.com/agentstax/vulkan/pkg/errors"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/maintain"
	"github.com/agentstax/vulkan/pkg/topic"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
	"github.com/agentstax/vulkan/pkg/worker"
	"github.com/agentstax/vulkan/pkg/worker/cronscheduler"
	"github.com/agentstax/vulkan/pkg/worker/janitor"
	"github.com/agentstax/vulkan/pkg/worker/manager"
	"github.com/agentstax/vulkan/pkg/worker/waterline"
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

	topicController   *topiccontroller.TopicController
	maintenance       *maintain.MaintenanceDatastore
	consumerDatastore *ConsumerDatastore[Message]
	abandonedEvents   *consumermetrics.MetricEventProducer
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
	if cfg.Type != CURSOR && cfg.Type != LIFECYCLE {
		return nil, fmt.Errorf("invalid consumer type %q", cfg.Type)
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
	consumerDatastore, err := NewConsumerDatastore[Message](ds, &ConsumerDatastoreConfig{
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
		Config:            cfg,
		Logger:            cfg.Logger,
		consumerGroup:     consumerGroup,
		topicName:         topicName,
		version:           version,
		ds:                ds,
		topicController:   topicController,
		maintenance:       maintenance,
		consumerDatastore: consumerDatastore,
		abandonedEvents:   abandonedEvents,
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

	group, err := c.consumerDatastore.RegisterGroup(ctx, current.Id, c.consumerGroup)
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

	permit, err := newConsumePermit(owner)
	if err != nil {
		return nil, err
	}

	return &ConsumerInstance[Message]{
		Owner:           owner,
		Config:          c.Config,
		Logger:          c.Logger,
		ds:              c.ds,
		abandonedEvents: c.abandonedEvents,
		permit:          permit,
		lifecycleCtx:    ctx,
	}, nil
}

// ConsumerInstance is a registered consumer group: Consume runs its manager,
// which spawns and heals every worker in the group's chain.
type ConsumerInstance[Message any] struct {
	Owner  *common.Owner
	Config *ConsumerConfig
	Logger logger.Logger

	ds              *datastore.PostgresDatastore
	abandonedEvents *consumermetrics.MetricEventProducer
	lifecycleCtx    context.Context
	permit          *consumePermit
}

// Consume blocks until stopped: cancel ctx to stop this call, or cancel the
// context given to Register to wind the whole instance down. Either requested
// stop shuts down in-flight work and returns nil; a runner's fatal error tears
// the instance down and returns here.
func (i *ConsumerInstance[Message]) Consume(ctx context.Context, consumerFunc ConsumerFunc[Message]) error {
	if consumerFunc == nil {
		return errors.New("consumerFunc must not be nil")
	}
	if err := i.lifecycleCtx.Err(); err != nil {
		return fmt.Errorf("%w: consumer group %q -- the lifetime context passed to Register is cancelled (%v)", vulkanerrors.ErrShutdownRequested, i.Owner.Name, err)
	}

	release, err := i.permit.acquire()
	if err != nil {
		return err
	}
	defer release()

	runCtx, cancel := mergeLifecycle(ctx, i.lifecycleCtx)
	defer cancel()

	runner, err := i.newManagerRunner(runCtx, consumerFunc)
	if err != nil {
		return err
	}

	i.Logger.InfoContext(runCtx, "consumer starting", "group", i.Owner.Name, "topic", i.Owner.TopicId)
	err = runner.Run(runCtx)
	if err == nil && runCtx.Err() != nil {
		i.Logger.InfoContext(context.WithoutCancel(runCtx), "consumer stopped", "reason", stopReason(runCtx), "group", i.Owner.Name, "topic", i.Owner.TopicId)
	}
	return err
}

// every group-owned row is declared here rather than at Register, so the
// definition that runs a row is the one that creates it and a second Consume
// re-creates whatever a crash lost
func (i *ConsumerInstance[Message]) newManagerRunner(ctx context.Context, consumerFunc ConsumerFunc[Message]) (*manager.Runner, error) {
	groupDefinitions, err := newGroupDefinitions(i.ds, consumerFunc, i.abandonedEvents, i.Config)
	if err != nil {
		return nil, err
	}
	topicDefinitions, err := newTopicDefinitions(i.ds, i.Config)
	if err != nil {
		return nil, err
	}

	provisioners := make([]worker.Provisioner, 0, len(groupDefinitions)+len(topicDefinitions))
	for _, definition := range groupDefinitions {
		if err := definition.Declare(ctx, i.Owner); err != nil {
			return nil, err
		}
		provisioners = append(provisioners, definition)
	}
	provisioners = append(provisioners, topicDefinitions...)

	managerDefinition, err := manager.NewManagerDefinition(i.ds, &manager.ManagerConfig{
		Logger: i.Config.Logger,
		Retry:  i.Config.Retry,
	}, provisioners...)
	if err != nil {
		return nil, err
	}

	if err := managerDefinition.Declare(ctx, i.Owner); err != nil {
		return nil, err
	}

	return manager.NewRunner(managerDefinition, i.Owner, &manager.RunnerConfig{
		Logger: i.Logger,
	})
}

// CURSOR -> one frontier per group, with a waterline rolling behind it
// LIFECYCLE -> state per delivery row, so no exception window and no waterline
func newGroupDefinitions[Message any](ds *datastore.PostgresDatastore, consumerFunc ConsumerFunc[Message], abandonedEvents *consumermetrics.MetricEventProducer, cfg *ConsumerConfig) ([]worker.Definition, error) {
	if cfg.Type != CURSOR {
		delivery, err := NewDeliveryConsumerDefinition(ds, consumerFunc, abandonedEvents, cfg)
		if err != nil {
			return nil, err
		}
		return []worker.Definition{delivery}, nil
	}

	message, err := NewMessageConsumerDefinition(ds, consumerFunc, abandonedEvents, cfg)
	if err != nil {
		return nil, err
	}
	exception, err := NewExceptionConsumerDefinition(ds, consumerFunc, abandonedEvents, cfg)
	if err != nil {
		return nil, err
	}
	waterlineDefinition, err := waterline.NewWaterlineDefinition(ds, &waterline.WaterlineConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}
	return []worker.Definition{message, exception, waterlineDefinition}, nil
}

func newTopicDefinitions(ds *datastore.PostgresDatastore, cfg *ConsumerConfig) ([]worker.Provisioner, error) {
	janitorDefinition, err := janitor.NewJanitorDefinition(ds, &janitor.JanitorConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}
	cronSchedulerDefinition, err := cronscheduler.NewCronSchedulerDefinition(ds, &cronscheduler.CronSchedulerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return []worker.Provisioner{cronSchedulerDefinition, janitorDefinition}, nil
}
