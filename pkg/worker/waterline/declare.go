package waterline

import (
	"context"

	"github.com/agentstax/vulkan/pkg/common"
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
