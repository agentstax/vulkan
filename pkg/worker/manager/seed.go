package manager

import (
	"context"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/worker/controller"
)

// Seed creates the system's manager worker row with the default tuning; an
// existing row is left untouched. Nothing spawns the manager, so it runs its
// own seed on every Run -- a seed lost to a crash heals on the next boot.
func (m *ManagerFactory) Seed(ctx context.Context, owner *common.Owner) error {
	if err := controller.ValidateOwner(owner, common.OwnerSystem, WorkerManager); err != nil {
		return err
	}

	return m.workers.InsertWorker(ctx, WorkerManager, owner, &controller.WorkerConfig{
		Metadata: defaultManagerMetadata(),
	})
}
