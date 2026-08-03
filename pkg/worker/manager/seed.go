package manager

import (
	"context"
	"errors"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/worker"
	"github.com/agentstax/vulkan/pkg/worker/controller"
)

// Seed creates owner's manager worker row; an existing row is left untouched,
// so a seed lost to a crash heals on the next register. Any owner kind seeds
// one, and no instance target -- every process reconciling owner's chain
// claims its own.
func (m *ManagerFactory) Seed(ctx context.Context, owner *common.Owner) error {
	if owner == nil {
		return errors.New("owner must not be nil")
	}

	return m.workers.InsertWorker(ctx, WorkerManager, owner, &controller.WorkerConfig{
		Metadata:        defaultManagerMetadata(),
		TargetInstances: worker.NoInstanceTarget,
	})
}
