package workerliveness

import (
	"context"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/consume"
	"github.com/agentstax/vulkan/pkg/migrate"
	"github.com/agentstax/vulkan/pkg/schedule"
	"github.com/agentstax/vulkan/pkg/worker"
	workercontroller "github.com/agentstax/vulkan/pkg/worker/controller"
)

// Declare creates the alert's consumer group on the schedules topic and its
// job-name binding declaration, then writes the alert's config onto the group's
// worker row -- the newest declaration wins. RegisterSystem runs it every time.
func (p *WorkerLivenessProvisioner) Declare(ctx context.Context, owner *common.Owner) error {
	if err := workercontroller.ValidateOwner(owner, common.OwnerSystem, JobName); err != nil {
		return err
	}

	jobRequestsTopic, err := p.topics.Get(ctx, schedule.TopicName)
	if err != nil {
		return err
	}
	if jobRequestsTopic == nil {
		return migrate.ErrNotRegistered.With("topic", schedule.TopicName)
	}

	group, err := p.consumers.RegisterGroup(ctx, jobRequestsTopic.Id, JobName, consume.Beginning())
	if err != nil {
		return err
	}

	// a waiting outcome is fine -- the consumer retries the declaration in Consume
	if _, err := p.consumers.DeclareBindings(ctx, jobRequestsTopic.Id, group.Id, []string{JobName}, time.Now()); err != nil {
		return err
	}

	groupOwner, err := common.NewConsumerGroupOwner(jobRequestsTopic.SystemId, jobRequestsTopic.Id, group.Id, group.Name)
	if err != nil {
		return err
	}
	return p.workers.DeclareWorker(ctx, p.definition, groupOwner)
}

// Provision claims one live instance. nil = declined (target_instances
// already filled) -- not an error, try again later.
func (p *WorkerLivenessProvisioner) Provision(ctx context.Context, declared *worker.WorkerData) (worker.Execution, error) {
	parsed, err := workercontroller.ParseMetadata[workerLivenessMetadata](declared.Metadata)
	if err != nil {
		return nil, err
	}
	if err := parsed.Validate(); err != nil {
		return nil, err
	}
	claimed, err := p.workers.RegisterInstance(ctx, declared.Id, declared.Owner, common.OwnerConsumerGroup, JobName, p.Config.InstanceTTL)
	if err != nil || claimed == nil {
		return nil, err
	}
	return newWorkerLivenessInstance(p, declared.Owner, claimed, parsed.RepeatInterval)
}
