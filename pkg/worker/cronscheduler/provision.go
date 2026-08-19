package cronscheduler

import (
	"context"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/cron"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/agentstax/vulkan/pkg/worker"
	"github.com/agentstax/vulkan/pkg/worker/controller"
)

// Declare creates the system's cron scheduler worker row and writes the default
// config onto it -- the newest declaration wins. Registers run it every time,
// so a declaration lost to a crash heals on the next one.
func (d *CronSchedulerDefinition) Declare(ctx context.Context, owner *common.Owner) error {
	if err := controller.ValidateOwner(owner, common.OwnerSystem, WorkerCronScheduler); err != nil {
		return err
	}

	return d.workers.RegisterWorker(ctx, WorkerCronScheduler, owner, &controller.WorkerConfig{
		Metadata: defaultCronSchedulerMetadata(),
	})
}

// Provision claims one live instance. nil = declined (target_instances
// already filled) -- not an error, try again later.
func (d *CronSchedulerDefinition) Provision(ctx context.Context, workerId int64, owner *common.Owner, metadata any) (worker.Execution, error) {
	parsed, err := controller.ParseMetadata[cronSchedulerMetadata](metadata)
	if err != nil {
		return nil, err
	}
	if err := parsed.Validate(); err != nil {
		return nil, err
	}

	// producer registration before the claim: a failure here leaves no
	// claimed instance behind to block reconciles until its TTL lapses
	producerInstance, err := d.producer.Register(ctx, cron.TopicName, topic.SchemaVersion(1))
	if err != nil {
		return nil, err
	}
	claimed, err := d.workers.RegisterInstance(ctx, workerId, owner, common.OwnerSystem, WorkerCronScheduler, d.Config.InstanceTTL)
	if err != nil || claimed == nil {
		return nil, err
	}
	return newCronSchedulerInstance(d, owner, claimed, parsed, producerInstance)
}
