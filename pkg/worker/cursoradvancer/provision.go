package cursoradvancer

import (
	"context"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/worker"
	"github.com/agentstax/vulkan/pkg/worker/controller"
)

// Declare creates the owner group's cursor advancer worker row and writes the
// default config onto it -- the newest declaration wins. Registers run it
// every time, so a declaration lost to a crash heals on the next one.
func (d *CursorAdvancerDefinition) Declare(ctx context.Context, owner *common.Owner) error {
	if err := controller.ValidateOwner(owner, common.OwnerConsumerGroup, WorkerCursorAdvancer); err != nil {
		return err
	}

	return d.workers.RegisterWorker(ctx, WorkerCursorAdvancer, owner, &controller.WorkerConfig{
		Metadata: defaultCursorAdvancerMetadata(),
	})
}

// Provision claims one live instance. nil = declined (target_instances
// already filled) -- not an error, try again later.
func (d *CursorAdvancerDefinition) Provision(ctx context.Context, workerId int64, owner *common.Owner, metadata any) (worker.Execution, error) {
	parsed, err := controller.ParseMetadata[cursorAdvancerMetadata](metadata)
	if err != nil {
		return nil, err
	}
	if err := parsed.Validate(); err != nil {
		return nil, err
	}
	claimed, err := d.workers.RegisterInstance(ctx, workerId, owner, common.OwnerConsumerGroup, WorkerCursorAdvancer, d.Config.InstanceTTL)
	if err != nil || claimed == nil {
		return nil, err
	}
	return newCursorAdvancerInstance(d, owner, claimed, parsed)
}
