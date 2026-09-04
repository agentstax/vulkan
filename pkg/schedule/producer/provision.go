package producer

import (
	"context"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/worker"
	"github.com/agentstax/vulkan/pkg/worker/controller"
)

// Declare writes the definition as the owner's worker row -- the newest
// declaration wins. Registers run it every time, so a declaration lost to a
// crash heals on the next one.
func (d *ScheduleProducerProvisioner) Declare(ctx context.Context, owner *common.Owner) error {
	return d.workers.DeclareWorker(ctx, d.definition, owner)
}

// Provision claims one live instance. nil = declined (target_instances
// already filled) -- not an error, try again later.
func (d *ScheduleProducerProvisioner) Provision(ctx context.Context, declared *worker.Worker) (worker.Execution, error) {
	parsed, err := controller.ParseMetadata[scheduleProducerMetadata](declared.Metadata)
	if err != nil {
		return nil, err
	}
	if err := parsed.Validate(); err != nil {
		return nil, err
	}

	claimed, err := d.workers.RegisterInstance(ctx, declared.Id, declared.Owner, common.OwnerSystem, WorkerScheduleProducer, d.Config.InstanceTTL)
	if err != nil || claimed == nil {
		return nil, err
	}
	return newScheduleProducerInstance(d, declared.Owner, claimed, parsed)
}
