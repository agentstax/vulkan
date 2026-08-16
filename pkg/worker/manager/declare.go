package manager

import (
	"context"
	"errors"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/worker"
	"github.com/agentstax/vulkan/pkg/worker/controller"
)

// Declare creates owner's manager worker row and writes the default tuning
// onto it -- the newest declaration wins. Registers run it every time, so a
// declaration lost to a crash heals on the next one. Any owner kind declares
// one, and no instance target -- every process reconciling owner's chain
// claims its own.
func (m *ManagerDefinition) Declare(ctx context.Context, owner *common.Owner) error {
	if owner == nil {
		return errors.New("owner must not be nil")
	}

	return m.workers.InsertWorker(ctx, WorkerManager, owner, &controller.WorkerConfig{
		Metadata:        defaultManagerMetadata(),
		TargetInstances: worker.NoInstanceTarget,
	})
}
