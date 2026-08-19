package janitor

import (
	"context"
	"fmt"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/worker"
	"github.com/agentstax/vulkan/pkg/worker/controller"
)

// Declare creates the owner topic's janitor worker row and writes the default
// config onto it -- the newest declaration wins. Registers run it every time,
// so a declaration lost to a crash heals on the next one.
func (j *JanitorDefinition) Declare(ctx context.Context, owner *common.Owner) error {
	if err := controller.ValidateOwner(owner, common.OwnerTopic, WorkerJanitor); err != nil {
		return err
	}

	return j.workers.RegisterWorker(ctx, WorkerJanitor, owner, &controller.WorkerConfig{
		Metadata: defaultJanitorMetadata(),
	})
}

// Provision claims one live instance. nil = declined (target_instances
// already filled) -- not an error, try again later.
func (j *JanitorDefinition) Provision(ctx context.Context, workerId int64, owner *common.Owner, metadata any) (worker.Execution, error) {
	parsed, err := controller.ParseMetadata[janitorMetadata](metadata)
	if err != nil {
		return nil, err
	}
	if err := parsed.Validate(); err != nil {
		return nil, err
	}

	// topic resolution before the claim: a failure here leaves no claimed
	// instance behind to block reconciles until its TTL lapses
	current, err := j.topics.GetTopicById(ctx, owner.TopicId)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, fmt.Errorf("topic %d not found -- register it with MessageAdmin.RegisterTopic first", owner.TopicId)
	}
	claimed, err := controller.RegisterInstance(ctx, j.workers, workerId, owner, common.OwnerTopic, WorkerJanitor, j.Config.InstanceTTL)
	if err != nil || claimed == nil {
		return nil, err
	}
	return newJanitorExecution(j, current, claimed, parsed)
}
