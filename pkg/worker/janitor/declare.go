package janitor

import (
	"context"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/worker/controller"
)

// Declare creates the owner topic's janitor worker row and writes the default
// tuning onto it -- the newest declaration wins. Registers run it every time,
// so a declaration lost to a crash heals on the next one.
func (j *JanitorDefinition) Declare(ctx context.Context, owner *common.Owner) error {
	if err := controller.ValidateOwner(owner, common.OwnerTopic, WorkerJanitor); err != nil {
		return err
	}

	return j.workers.RegisterWorker(ctx, WorkerJanitor, owner, &controller.WorkerConfig{
		Metadata: defaultJanitorMetadata(),
	})
}
