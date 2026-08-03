package consumer

import (
	"context"
	"errors"
	"fmt"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/concurrency"
	"github.com/agentstax/vulkan/pkg/datastore"
	vulkanerrors "github.com/agentstax/vulkan/pkg/errors"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/agentstax/vulkan/pkg/worker/manager"
	"golang.org/x/sync/errgroup"
)

// ideally idempotent func
type ConsumerFunc[Message any] func(ctx context.Context, message *Message) error

// Consumer runs a consumer group on one topic: hand Consume a function and
// every message reaches it. Failed messages retry with backoff, and the
// topic's upkeep (partitions, retention, waterline) happens automatically.
type Consumer[Message any] struct {
	MessageConsumer   *MessageConsumer[Message]   // nil when Config.Type is LIFECYCLE
	ExceptionConsumer *ExceptionConsumer[Message] // nil when Config.Type is LIFECYCLE
	DeliveryConsumer  *DeliveryConsumer[Message]  // nil when Config.Type is CURSOR
	Config            *ConsumerConfig
	Logger            logger.Logger // copied from Config.Logger at construction

	consumerGroup string
	topicName     string
	managerRunner *manager.Runner
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewConsumer[Message any](consumerGroup string, topicName string, version topic.SchemaVersion, queue concurrency.Queue[Buffered], poolLimiter concurrency.PoolLimiter, ds *datastore.PostgresDatastore, cfg *ConsumerConfig) (*Consumer[Message], error) {
	if cfg == nil {
		cfg = &ConsumerConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	consumer := &Consumer[Message]{
		Config:        cfg,
		Logger:        cfg.Logger,
		consumerGroup: consumerGroup,
		topicName:     topicName,
	}

	var err error
	switch cfg.Type {
	case CURSOR:
		consumer.MessageConsumer, err = NewMessageConsumer[Message](consumerGroup, topicName, version, queue, poolLimiter, ds, cfg)
		if err != nil {
			return nil, err
		}
		consumer.ExceptionConsumer, err = NewExceptionConsumer[Message](consumerGroup, topicName, version, ds, cfg)
		if err != nil {
			return nil, err
		}
	case LIFECYCLE:
		consumer.DeliveryConsumer, err = NewDeliveryConsumer[Message](consumerGroup, topicName, version, ds, cfg)
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("invalid consumer type %q", cfg.Type)
	}

	return consumer, nil
}

// Register resolves this consumer's topic by name against the live topic row,
// sets up its cursor, and starts the consumer's lifecycle.
//
// ctx must be cancellable, unless ConsumerConfig.DisableGracefulShutdown
// declares otherwise.
func (c *Consumer[Message]) Register(ctx context.Context) error {
	// each part registers itself -- the overlapping bootstrap (topic
	// resolution, schema assert, cursor upsert) is idempotent
	switch c.Config.Type {
	case CURSOR:
		if err := c.MessageConsumer.Register(ctx); err != nil {
			return err
		}
		if err := c.ExceptionConsumer.Register(ctx); err != nil {
			return err
		}
	case LIFECYCLE:
		if err := c.DeliveryConsumer.Register(ctx); err != nil {
			return err
		}
	}
	// the group's row, not the topic's -- the chain it sits on excludes
	// sibling groups
	t := c.base().Topic
	group := c.base().Group
	groupOwner, err := common.NewConsumerGroupOwner(t.SystemId, t.Id, group.Id, group.Name)
	if err != nil {
		return err
	}
	c.managerRunner, err = manager.NewRunner(c.base().manager, groupOwner, &manager.RunnerConfig{
		Logger: c.Logger,
	})
	return err
}

// base is the registered part whose base carries this consumer's lifecycle.
func (c *Consumer[Message]) base() *consumerBase[Message] {
	if c.Config.Type == LIFECYCLE {
		return c.DeliveryConsumer.consumerBase
	}
	return c.MessageConsumer.consumerBase
}

// Consume claims and processes messages with consumerFunc, blocking until
// stopped: cancel ctx to stop this call, or cancel the context given to
// Register to wind the whole consumer down. A requested stop from either side
// shuts down in-flight work and returns nil
func (c *Consumer[Message]) Consume(ctx context.Context, consumerFunc ConsumerFunc[Message]) error {
	base := c.base()
	if err := base.lifecycleErr(); err != nil {
		return err
	}
	runCtx, cancel := base.runCtx(ctx)
	defer cancel()

	c.Logger.InfoContext(runCtx, "consumer starting", "group", c.consumerGroup, "topic", c.topicName, "version", base.version)

	g, gCtx := errgroup.WithContext(runCtx)

	switch c.Config.Type {
	case CURSOR:
		g.Go(func() error {
			return c.MessageConsumer.Consume(gCtx, consumerFunc)
		})
		g.Go(func() error {
			return c.ExceptionConsumer.Consume(gCtx, consumerFunc)
		})
	case LIFECYCLE:
		g.Go(func() error {
			return c.DeliveryConsumer.Consume(gCtx, consumerFunc)
		})
	}

	g.Go(func() error {
		return c.managerRunner.Run(gCtx)
	})

	err := g.Wait()
	if err == nil && runCtx.Err() != nil {
		// requested shutdown (either side), not a failure -- log which side asked
		reason := "caller context cancelled"
		if errors.Is(context.Cause(runCtx), vulkanerrors.ErrShutdownRequested) {
			reason = "lifecycle context cancelled"
		}
		c.Logger.InfoContext(ctx, "consumer stopped", "reason", reason, "group", c.consumerGroup, "topic", c.topicName, "version", base.version)
	}
	return err
}
