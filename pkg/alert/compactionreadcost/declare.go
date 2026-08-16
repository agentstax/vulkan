package compactionreadcost

import (
	"context"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/cron"
	"github.com/agentstax/vulkan/pkg/topic"
	workercontroller "github.com/agentstax/vulkan/pkg/worker/controller"
)

// Declare creates the alert's consumer group on the job_requests topic and its
// job-name binding declaration, then writes the alert's config onto the group's
// worker row -- the newest declaration wins. RegisterSystem runs it every time.
func (d *CompactionReadCostDefinition) Declare(ctx context.Context, owner *common.Owner) error {
	if err := workercontroller.ValidateOwner(owner, common.OwnerSystem, JobName); err != nil {
		return err
	}

	cronTopic, err := d.topics.GetTopic(ctx, cron.TopicName, topic.SchemaVersion(1))
	if err != nil {
		return err
	}
	if cronTopic == nil {
		return fmt.Errorf("%w: topic %q -- RegisterSystem creates it before declaring alert consumers", topic.ErrTopicNotFound, cron.TopicName)
	}

	group, err := d.consumers.RegisterGroup(ctx, cronTopic.Id, JobName)
	if err != nil {
		return err
	}

	// a waiting outcome is fine -- the consumer retries the declaration in Consume
	if _, err := d.consumers.DeclareBindings(ctx, group.Id, []string{JobName}, time.Now()); err != nil {
		return err
	}

	groupOwner, err := common.NewConsumerGroupOwner(cronTopic.SystemId, cronTopic.Id, group.Id, group.Name)
	if err != nil {
		return err
	}
	return d.workers.InsertWorker(ctx, JobName, groupOwner, &workercontroller.WorkerConfig{
		Metadata: toCompactionReadCostMetadata(d.Config),
	})
}
