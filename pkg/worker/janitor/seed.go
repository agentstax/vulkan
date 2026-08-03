package janitor

import (
	"context"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/worker/controller"
)

// Seed creates the owner topic's janitor worker row with the default tuning;
// an existing row is left untouched, so registers run it every time -- a
// seed lost to a crash heals on the next one.
func (j *JanitorFactory) Seed(ctx context.Context, owner *common.Owner) error {
	if err := controller.ValidateOwner(owner, common.OwnerTopic, WorkerJanitor); err != nil {
		return err
	}

	return j.workers.InsertWorker(ctx, WorkerJanitor, owner, &controller.WorkerConfig{
		Metadata: defaultJanitorMetadata(),
	})
}
