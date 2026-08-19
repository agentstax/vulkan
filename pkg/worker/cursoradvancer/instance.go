package cursoradvancer

import (
	"context"
	"errors"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/worker"
	"github.com/agentstax/vulkan/pkg/worker/controller"
	cursoradvancercontroller "github.com/agentstax/vulkan/pkg/worker/cursoradvancer/controller"
)

// advances cursor.committed behind the group's resolved work at the row's
// poll_rate while a heartbeat holds the claim
type CursorAdvancerInstance struct {
	Owner  *common.Owner
	Config *CursorAdvancerConfig
	Logger common.Logger

	runner     *controller.InstanceTickRunner
	controller *cursoradvancercontroller.CursorAdvancerController
	metadata   *cursorAdvancerMetadata
}

func newCursorAdvancerInstance(definition *CursorAdvancerDefinition, owner *common.Owner, claimed *worker.WorkerInstance, metadata *cursorAdvancerMetadata) (*CursorAdvancerInstance, error) {
	if owner == nil {
		return nil, errors.New("owner must not be nil")
	}
	if metadata == nil {
		return nil, errors.New("metadata must not be nil")
	}

	runner, err := controller.NewInstanceTickRunner(definition.workers, claimed, metadata.PollRate, &controller.InstanceTickRunnerConfig{
		InstanceTTL:    definition.Config.InstanceTTL,
		JitterFraction: definition.Config.JitterFraction,
		Logger:         common.LoggerWith(definition.Logger, "worker", WorkerCursorAdvancer, "topic", owner.TopicId, "group", owner.Name),
		TickRetry:      definition.Config.AdvanceRetry,
	})
	if err != nil {
		return nil, err
	}

	return &CursorAdvancerInstance{
		Owner:      owner,
		Config:     definition.Config,
		Logger:     definition.Logger,
		runner:     runner,
		controller: definition.controller,
		metadata:   metadata,
	}, nil
}

// Run advances committed until ctx cancels; a requested stop returns nil. The claimed
// instance releases on the way out however Run exits.
func (i *CursorAdvancerInstance) Run(ctx context.Context) error {
	i.Logger.InfoContext(ctx, "cursor advancer starting", "topic", i.Owner.TopicId, "group", i.Owner.Name, "rate", i.metadata.PollRate)

	err := i.runner.Run(ctx, i.advance)
	if err == nil {
		i.Logger.InfoContext(ctx, "cursor advancer stopped", "topic", i.Owner.TopicId, "group", i.Owner.Name)
	}
	return err
}

func (i *CursorAdvancerInstance) advance(ctx context.Context) error {
	_, err := i.controller.AdvanceCommitted(ctx, i.Owner.TopicId, i.Owner.ConsumerGroupId)
	return err
}
