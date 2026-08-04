package waterline

import (
	"context"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/worker/controller"
)

// Declare creates the owner group's waterline worker row with the default
// tuning; an existing row is left untouched, so registers run it every time
// -- a declaration lost to a crash heals on the next one.
func (w *WaterlineDefinition) Declare(ctx context.Context, owner *common.Owner) error {
	if err := controller.ValidateOwner(owner, common.OwnerConsumerGroup, WorkerWaterline); err != nil {
		return err
	}

	return w.workers.InsertWorker(ctx, WorkerWaterline, owner, &controller.WorkerConfig{
		Metadata: defaultWaterlineMetadata(),
	})
}
