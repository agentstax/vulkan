package cronscheduler

import (
	"context"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/worker/controller"
)

// Seed creates the system's cron scheduler worker row with the default tuning;
// an existing row is left untouched, so registers run it every time -- a
// seed lost to a crash heals on the next one.
func (s *CronSchedulerFactory) Seed(ctx context.Context, owner *common.Owner) error {
	if err := controller.ValidateOwner(owner, common.OwnerSystem, WorkerCronScheduler); err != nil {
		return err
	}

	return s.workers.InsertWorker(ctx, WorkerCronScheduler, owner, &controller.WorkerConfig{
		Metadata: defaultCronSchedulerMetadata(),
	})
}
