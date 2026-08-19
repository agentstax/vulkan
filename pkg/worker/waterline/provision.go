package waterline

import (
	"context"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/worker"
	"github.com/agentstax/vulkan/pkg/worker/controller"
)

// Declare creates the owner group's waterline worker row and writes the
// default config onto it -- the newest declaration wins. Registers run it
// every time, so a declaration lost to a crash heals on the next one.
func (w *WaterlineDefinition) Declare(ctx context.Context, owner *common.Owner) error {
	if err := controller.ValidateOwner(owner, common.OwnerConsumerGroup, WorkerWaterline); err != nil {
		return err
	}

	return w.workers.RegisterWorker(ctx, WorkerWaterline, owner, &controller.WorkerConfig{
		Metadata: defaultWaterlineMetadata(),
	})
}

// Provision claims one live instance. nil = declined (target_instances
// already filled) -- not an error, try again later.
func (w *WaterlineDefinition) Provision(ctx context.Context, workerId int64, owner *common.Owner, metadata any) (worker.Execution, error) {
	parsed, err := controller.ParseMetadata[waterlineMetadata](metadata)
	if err != nil {
		return nil, err
	}
	if err := parsed.Validate(); err != nil {
		return nil, err
	}
	claimed, err := w.workers.RegisterInstance(ctx, workerId, owner, common.OwnerConsumerGroup, WorkerWaterline, w.Config.InstanceTTL)
	if err != nil || claimed == nil {
		return nil, err
	}
	return newWaterlineExecution(w, owner, claimed, parsed)
}
