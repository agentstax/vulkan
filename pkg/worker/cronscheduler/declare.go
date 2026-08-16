package cronscheduler

import (
	"context"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/worker/controller"
)

// Declare creates the system's cron scheduler worker row and writes the default
// config onto it -- the newest declaration wins. Registers run it every time,
// so a declaration lost to a crash heals on the next one.
func (s *CronSchedulerDefinition) Declare(ctx context.Context, owner *common.Owner) error {
	if err := controller.ValidateOwner(owner, common.OwnerSystem, WorkerCronScheduler); err != nil {
		return err
	}

	return s.workers.RegisterWorker(ctx, WorkerCronScheduler, owner, &controller.WorkerConfig{
		Metadata: defaultCronSchedulerMetadata(),
	})
}
