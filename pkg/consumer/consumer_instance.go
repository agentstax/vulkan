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
	"golang.org/x/sync/errgroup"
)

// ConsumerInstance is a registered consumer group: Consume runs its manager,
// which spawns and heals every worker in the group's chain.
type ConsumerInstance[Message any] struct {
	Owner  *common.Owner
	Config *ConsumerConfig
	Logger logger.Logger

	ds              *datastore.PostgresDatastore
	abandonedEvents *consumermetrics.MetricEventProducer
	permit          *consumePermit
}

// cfg arrives already resolved by NewConsumer -- Register is the only caller,
// so there is nothing left to default or validate here.
func NewConsumerInstance[Message any](owner *common.Owner, ds *datastore.PostgresDatastore, abandonedEvents *consumermetrics.MetricEventProducer, cfg *ConsumerConfig) (*ConsumerInstance[Message], error) {
	if owner == nil {
		return nil, errors.New("owner must not be nil")
	}
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}
	if abandonedEvents == nil {
		return nil, errors.New("abandonedEvents must not be nil")
	}
	if cfg == nil {
		return nil, errors.New("config must not be nil")
	}

	permit, err := newConsumePermit(owner)
	if err != nil {
		return nil, err
	}

	return &ConsumerInstance[Message]{
		Owner:           owner,
		Config:          cfg,
		Logger:          cfg.Logger,
		ds:              ds,
		abandonedEvents: abandonedEvents,
		permit:          permit,
	}, nil
}

// Consume blocks until stopped: ctx is the instance's lifetime, cancel it to
// shut down in-flight work and return nil. A runner's fatal error tears the
// instance down and returns here. ctx must be cancellable, unless
// ConsumerConfig.DisableGracefulShutdown declares otherwise.
func (i *ConsumerInstance[Message]) Consume(ctx context.Context, consumerFunc ConsumerFunc[Message]) error {
	if consumerFunc == nil {
		return errors.New("consumerFunc must not be nil")
	}
	// Done() == nil -> Background/TODO -> no cancel can ever arrive, so the
	// shutdown phase would silently not exist
	if ctx.Done() == nil && !i.Config.DisableGracefulShutdown {
		return fmt.Errorf("%w: consumer group %q\n%s", vulkanerrors.ErrLifecycleContextNotCancellable, i.Owner.Name, lifecycleContextHelp)
	}

	release, err := i.permit.acquire()
	if err != nil {
		return err
	}
	defer release()

	runner, err := i.newManagerRunner(ctx, consumerFunc)
	if err != nil {
		return err
	}

	i.Logger.InfoContext(ctx, "consumer starting", "group", i.Owner.Name, "topic", i.Owner.TopicId)

	group, runCtx := errgroup.WithContext(ctx)
	// abandonedEvents.Run goes beside the manager, abandonedEvents
	// arrive as consumers shut down, after claim work is done
	group.Go(func() error {
		return i.abandonedEvents.Run(runCtx)
	})
	group.Go(func() error {
		return runner.Run(runCtx)
	})

	err = group.Wait()
	if err == nil && ctx.Err() != nil {
		i.Logger.InfoContext(context.WithoutCancel(ctx), "consumer stopped", "group", i.Owner.Name, "topic", i.Owner.TopicId)
	}
	return err
}
