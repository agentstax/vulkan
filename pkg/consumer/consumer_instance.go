package consumer

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/common/logging"
	"github.com/agentstax/vulkan/pkg/concurrency"
	"github.com/agentstax/vulkan/pkg/consumergroup"
	consumergroupcontroller "github.com/agentstax/vulkan/pkg/consumergroup/controller"
	"github.com/agentstax/vulkan/pkg/datastore"
	metricsproducer "github.com/agentstax/vulkan/pkg/metrics/producer"
	"golang.org/x/sync/errgroup"
)

// ConsumerInstance is a registered consumer group: Consume runs its manager,
// which spawns and heals every worker in the group's chain.
type ConsumerInstance[Message any] struct {
	Owner  *common.Owner
	Config *ConsumerConfig
	Logger logging.Logger

	ds              *datastore.PostgresDatastore
	abandonedEvents *metricsproducer.MetricsProducer
	consumers       *consumergroupcontroller.ConsumerGroupController
	bindings        []string
	declaredAt      time.Time
	permit          *concurrency.Permit // held for the length of a Consume call
}

// cfg arrives already resolved by NewConsumer -- Register is the only caller,
// so there is nothing left to default or validate here.
// bindings and declaredAt are Register's declaration, re-attempted by Consume.
func newConsumerInstance[Message any](owner *common.Owner, ds *datastore.PostgresDatastore, abandonedEvents *metricsproducer.MetricsProducer, consumers *consumergroupcontroller.ConsumerGroupController, bindings []string, declaredAt time.Time, cfg *ConsumerConfig) (*ConsumerInstance[Message], error) {
	if owner == nil {
		return nil, errors.New("owner must not be nil")
	}
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}
	if abandonedEvents == nil {
		return nil, errors.New("abandonedEvents must not be nil")
	}
	if consumers == nil {
		return nil, errors.New("consumers must not be nil")
	}
	if declaredAt.IsZero() {
		return nil, errors.New("declaredAt is required")
	}
	if cfg == nil {
		return nil, errors.New("config must not be nil")
	}

	permit, err := concurrency.NewPermit()
	if err != nil {
		return nil, err
	}

	return &ConsumerInstance[Message]{
		Owner:           owner,
		Config:          cfg,
		Logger:          cfg.Logger,
		ds:              ds,
		abandonedEvents: abandonedEvents,
		consumers:       consumers,
		bindings:        bindings,
		declaredAt:      declaredAt,
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
		return fmt.Errorf("%w\n%s", common.ErrLifecycleContextNotCancellable.With("group", i.Owner.Name), lifecycleContextHelp)
	}

	release, ok := i.permit.Acquire()
	if !ok {
		return common.ErrAlreadyConsuming.With("group", i.Owner.Name, "topic_id", i.Owner.TopicId)
	}
	defer release()

	// blocking until bindings install or join
	if err := i.declareBindings(ctx); err != nil {
		// a cancel during the wait is a requested stop, not a failure
		if ctx.Err() != nil {
			return nil
		}
		return err
	}

	runner, err := i.newManagerRunner(ctx, consumerFunc)
	if err != nil {
		return err
	}

	i.Logger.InfoContext(ctx, "consumer starting", "group", i.Owner.Name, "topic_id", i.Owner.TopicId, "vulkan_version", common.BuildVersion(), "message_timeout", i.Config.Message.Timeout, "shutdown_timeout", i.Config.ShutdownTimeout, "batch_limit", i.Config.BatchLimit)

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
		i.Logger.InfoContext(context.WithoutCancel(ctx), "consumer stopped", "group", i.Owner.Name, "topic_id", i.Owner.TopicId)
	}
	return err
}

// declareBindings retries the declaration until it is installed or joined.
func (i *ConsumerInstance[Message]) declareBindings(ctx context.Context) error {
	for attempt := 1; ; attempt++ {
		// Register's outcome is not trusted -- another declarer may have
		// replaced the set while this instance had no live heartbeat
		outcome, err := i.consumers.DeclareBindings(ctx, i.Owner.ConsumerGroupId, i.bindings, i.declaredAt)
		if err != nil {
			return err
		}
		if outcome != consumergroup.DeclarationWaiting {
			return nil
		}

		i.Logger.WarnContext(ctx, "binding declaration waiting -- a live instance still declares a different set",
			"group", i.Owner.Name,
			"patterns", i.bindings,
			"attempt", attempt,
			"elapsed", time.Since(i.declaredAt).Round(time.Second))

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(i.Config.BindingRetryInterval):
		}
	}
}
