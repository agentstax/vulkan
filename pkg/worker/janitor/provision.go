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
func (d *JanitorDefinition) Declare(ctx context.Context, owner *common.Owner) error {
	if err := controller.ValidateOwner(owner, common.OwnerTopic, WorkerJanitor); err != nil {
		return err
	}

	return d.workers.RegisterWorker(ctx, WorkerJanitor, owner, &controller.WorkerConfig{
		Metadata: defaultJanitorMetadata(),
	})
}

// Provision claims one live instance. nil = declined (target_instances
// already filled) -- not an error, try again later.
func (d *JanitorDefinition) Provision(ctx context.Context, workerId int64, owner *common.Owner, metadata any) (worker.Execution, error) {
	// owner is read before the claim (topic resolution below), so its check
	// cannot wait for RegisterInstance's
	if err := controller.ValidateOwner(owner, common.OwnerTopic, WorkerJanitor); err != nil {
		return nil, err
	}
	parsed, err := controller.ParseMetadata[janitorMetadata](metadata)
	if err != nil {
		return nil, err
	}
	if err := parsed.Validate(); err != nil {
		return nil, err
	}

	// topic resolution before the claim: a failure here leaves no claimed
	// instance behind to block reconciles until its TTL lapses
	current, err := d.topics.GetById(ctx, owner.TopicId)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, fmt.Errorf("topic %d not found -- register it with MessageAdmin.RegisterTopic first", owner.TopicId)
	}
	claimed, err := d.workers.RegisterInstance(ctx, workerId, owner, common.OwnerTopic, WorkerJanitor, d.Config.InstanceTTL)
	if err != nil || claimed == nil {
		return nil, err
	}
	return newJanitorInstance(d, current, claimed, parsed)
}
