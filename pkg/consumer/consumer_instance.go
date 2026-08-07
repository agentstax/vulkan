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
)

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

// cfg arrives already resolved by NewConsumer -- Register is the only caller,
// so there is nothing left to default or validate here.
func NewConsumerInstance[Message any](owner *common.Owner, ds *datastore.PostgresDatastore, abandonedEvents *consumermetrics.MetricEventProducer, lifecycleCtx context.Context, cfg *ConsumerConfig) (*ConsumerInstance[Message], error) {
	if owner == nil {
		return nil, errors.New("owner must not be nil")
	}
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}
	if abandonedEvents == nil {
		return nil, errors.New("abandonedEvents must not be nil")
	}
	if lifecycleCtx == nil {
		return nil, errors.New("lifecycleCtx must not be nil")
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
		lifecycleCtx:    lifecycleCtx,
	}, nil
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

// the merged context is done when EITHER ctx or lifecycleCtx cancels. Only a
// lifecycleCtx cancellation carries ErrShutdownRequested as the cause, which is
// how an exit tells app shutdown from the caller's own cancel.
func mergeLifecycle(ctx context.Context, lifecycleCtx context.Context) (context.Context, context.CancelFunc) {
	merged, cancel := context.WithCancelCause(ctx)
	stopWatch := context.AfterFunc(lifecycleCtx, func() {
		cancel(vulkanerrors.ErrShutdownRequested)
	})

	mergedCancel := func() {
		stopWatch() // unregister the AfterFunc (doesn't trigger it)
		cancel(nil) // nil cause = routine shutdown, ctx.Err() returns context.Canceled
	}

	return merged, mergedCancel
}

func stopReason(runCtx context.Context) string {
	if errors.Is(context.Cause(runCtx), vulkanerrors.ErrShutdownRequested) {
		return "lifecycle context cancelled"
	}
	return "caller context cancelled"
}
