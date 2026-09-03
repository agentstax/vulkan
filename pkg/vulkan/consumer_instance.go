package vulkan

import (
	"context"
	"errors"

	"github.com/agentstax/vulkan/pkg/consumer"
	"github.com/agentstax/vulkan/pkg/systemmanager"
	"golang.org/x/sync/errgroup"
)

// ConsumerInstance is a registered consumer group whose Consume also keeps
// the deployment's upkeep running: the system manager runs beside the
// session unless ClientConfig.DisableManager opted out.
type ConsumerInstance[Message Versioned] struct {
	*consumer.ConsumerInstance[Message]

	manager    *systemmanager.SystemManager
	runManager bool
}

func newConsumerInstance[Message Versioned](instance *consumer.ConsumerInstance[Message], manager *systemmanager.SystemManager, runManager bool) (*ConsumerInstance[Message], error) {
	if instance == nil {
		return nil, errors.New("instance must not be nil")
	}
	if manager == nil {
		return nil, errors.New("manager must not be nil")
	}
	return &ConsumerInstance[Message]{ConsumerInstance: instance, manager: manager, runManager: runManager}, nil
}

// Consume runs the group's session and the system manager beside it. A
// manager error before its first claim tears the session down; after that,
// manager failures are logged and retried, never returned (SystemManager.Run).
func (i *ConsumerInstance[Message]) Consume(ctx context.Context, consumerFunc ConsumerFunc[Message], options *ConsumeOptions) error {
	if !i.runManager {
		return i.ConsumerInstance.Consume(ctx, consumerFunc, options)
	}

	// if we can't cancel the context then we can't rely on errgroup
	// to correctly stop manager. So if user has an uncancellable context
	// AND they have DisabledGracefulShutdown let them run consumer only.
	if ctx.Done() == nil {
		return i.ConsumerInstance.Consume(ctx, consumerFunc, options)
	}

	// the session owns the pairing: whenever it returns -- nil included --
	// the manager stops; a manager error cancels runCtx so the session
	// drains too
	group, runCtx := errgroup.WithContext(ctx)
	managerCtx, stopManager := context.WithCancel(runCtx)
	group.Go(func() error {
		defer stopManager()
		return i.ConsumerInstance.Consume(runCtx, consumerFunc, options)
	})
	group.Go(func() error {
		return i.manager.Run(managerCtx)
	})
	return group.Wait()
}
