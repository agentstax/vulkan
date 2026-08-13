package partitioncount

import (
	"context"
	"fmt"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/cron"
	"github.com/agentstax/vulkan/pkg/topic"
	workercontroller "github.com/agentstax/vulkan/pkg/worker/controller"
)

// Declare creates the alert's consumer group on the job_requests topic, bound
// to exactly its job name, and the group's worker row; existing rows are left
// untouched, so RegisterSystem runs it every time.
func (d *PartitionCountDefinition) Declare(ctx context.Context, owner *common.Owner) error {
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
	if err := d.consumers.Bind(ctx, group.Id, JobName); err != nil {
		return err
	}

	groupOwner, err := common.NewConsumerGroupOwner(cronTopic.SystemId, cronTopic.Id, group.Id, group.Name)
	if err != nil {
		return err
	}
	return d.workers.InsertWorker(ctx, JobName, groupOwner, nil)
}
