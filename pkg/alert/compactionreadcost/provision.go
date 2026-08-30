package compactionreadcost

import (
	"context"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/consumergroup"
	"github.com/agentstax/vulkan/pkg/cron"
	"github.com/agentstax/vulkan/pkg/migrate"
	"github.com/agentstax/vulkan/pkg/worker"
	workercontroller "github.com/agentstax/vulkan/pkg/worker/controller"
)

// Declare creates the alert's consumer group on the job_requests topic and its
// job-name binding declaration, then writes the alert's config onto the group's
// worker row -- the newest declaration wins. RegisterSystem runs it every time.
func (d *CompactionReadCostProvisioner) Declare(ctx context.Context, owner *common.Owner) error {
	if err := workercontroller.ValidateOwner(owner, common.OwnerSystem, JobName); err != nil {
		return err
	}

	cronTopic, err := d.topics.Get(ctx, cron.TopicName)
	if err != nil {
		return err
	}
	if cronTopic == nil {
		return migrate.ErrNotRegistered.With("topic", cron.TopicName)
	}

	group, err := d.consumers.RegisterGroup(ctx, cronTopic.Id, JobName, consumergroup.Beginning())
	if err != nil {
		return err
	}

	// a waiting outcome is fine -- the consumer retries the declaration in Consume
	if _, err := d.consumers.DeclareBindings(ctx, cronTopic.Id, group.Id, []string{JobName}, time.Now()); err != nil {
		return err
	}

	groupOwner, err := common.NewConsumerGroupOwner(cronTopic.SystemId, cronTopic.Id, group.Id, group.Name)
	if err != nil {
		return err
	}
	return d.workers.DeclareWorker(ctx, d.definition, groupOwner)
}

// Provision claims one live instance. nil = declined (target_instances
// already filled) -- not an error, try again later.
func (d *CompactionReadCostProvisioner) Provision(ctx context.Context, declared *worker.Worker) (worker.Execution, error) {
	parsed, err := workercontroller.ParseMetadata[compactionReadCostMetadata](declared.Metadata)
	if err != nil {
		return nil, err
	}
	if err := parsed.Validate(); err != nil {
		return nil, err
	}
	claimed, err := d.workers.RegisterInstance(ctx, declared.Id, declared.Owner, common.OwnerConsumerGroup, JobName, d.Config.InstanceTTL)
	if err != nil || claimed == nil {
		return nil, err
	}
	return newCompactionReadCostInstance(d, declared.Owner, claimed, parsed.RepeatInterval)
}
