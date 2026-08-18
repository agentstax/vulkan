package waterline

import (
	"context"
	"errors"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/worker"
	"github.com/agentstax/vulkan/pkg/worker/controller"
	waterlinecontroller "github.com/agentstax/vulkan/pkg/worker/waterline/controller"
)

// rolls cursor.committed up behind the group's resolved work at the row's
// poll_rate while a heartbeat holds the claim
type WaterlineExecution struct {
	Owner  *common.Owner
	Config *WaterlineConfig
	Logger common.Logger

	runner     *controller.InstanceTickRunner
	controller *waterlinecontroller.WaterlineController
	metadata   *waterlineMetadata
}

func newWaterlineExecution(waterline *WaterlineDefinition, owner *common.Owner, claimed *worker.WorkerInstance, metadata *waterlineMetadata) (*WaterlineExecution, error) {
	if owner == nil {
		return nil, errors.New("owner must not be nil")
	}
	if metadata == nil {
		return nil, errors.New("metadata must not be nil")
	}

	runner, err := controller.NewInstanceTickRunner(waterline.workers, claimed, metadata.PollRate, &controller.InstanceTickRunnerConfig{
		InstanceTTL:    waterline.Config.InstanceTTL,
		JitterFraction: waterline.Config.JitterFraction,
		Logger:         common.LoggerWith(waterline.Logger, "worker", WorkerWaterline, "topic", owner.TopicId, "group", owner.Name),
		TickRetry:      waterline.Config.RollRetry,
	})
	if err != nil {
		return nil, err
	}

	return &WaterlineExecution{
		Owner:      owner,
		Config:     waterline.Config,
		Logger:     waterline.Logger,
		runner:     runner,
		controller: waterline.controller,
		metadata:   metadata,
	}, nil
}

// Run rolls until ctx cancels; a requested stop returns nil. The claimed
// instance releases on the way out however Run exits.
func (i *WaterlineExecution) Run(ctx context.Context) error {
	i.Logger.InfoContext(ctx, "waterline starting", "topic", i.Owner.TopicId, "group", i.Owner.Name, "rate", i.metadata.PollRate)

	err := i.runner.Run(ctx, i.roll)
	if err == nil {
		i.Logger.InfoContext(ctx, "waterline stopped", "topic", i.Owner.TopicId, "group", i.Owner.Name)
	}
	return err
}

// roll is one waterline pass.
func (i *WaterlineExecution) roll(ctx context.Context) error {
	_, err := i.controller.AdvanceWaterline(ctx, i.Owner.TopicId, i.Owner.ConsumerGroupId)
	return err
}
