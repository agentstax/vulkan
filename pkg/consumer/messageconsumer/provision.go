package messageconsumer

import (
	"context"

	"github.com/agentstax/vulkan/pkg/common"
	consumerbase "github.com/agentstax/vulkan/pkg/consumer/base"
	"github.com/agentstax/vulkan/pkg/worker"
	workercontroller "github.com/agentstax/vulkan/pkg/worker/controller"
)

// Declare creates this kind's worker row and writes the config onto it --
// the newest declaration wins.
func (d *MessageConsumerDefinition[Message]) Declare(ctx context.Context, owner *common.Owner) error {
	return d.DeclareWorker(ctx, owner, toMessageConsumerMetadata(d.Config))
}

// a nil Execution is a declined claim, not an error -- try again later.
func (d *MessageConsumerDefinition[Message]) Provision(ctx context.Context, workerId int64, owner *common.Owner, metadata any) (worker.Execution, error) {
	parsed, err := workercontroller.ParseMetadata[messageConsumerMetadata](metadata)
	if err != nil {
		return nil, err
	}
	if err := parsed.Validate(); err != nil {
		return nil, err
	}
	claimed, err := d.RegisterInstance(ctx, workerId, owner, d.Config.InstanceTTL)
	if err != nil || claimed == nil {
		return nil, err
	}

	cfg := d.Config.withMetadata(ctx, parsed)
	resolvedTopic, err := d.GetTopic(ctx, owner.TopicId)
	if err != nil {
		return nil, err
	}

	base, err := consumerbase.NewBaseConsumer(d.BaseDefinition, owner, resolvedTopic, &consumerbase.BaseConsumerConfig{
		TimeoutGrace: cfg.TimeoutGrace,
		RecordMargin: cfg.RecordMargin,
	})
	if err != nil {
		return nil, err
	}

	runner, err := newMessageRunner(base, d.consumers, cfg)
	if err != nil {
		return nil, err
	}

	return consumerbase.NewBaseInstance(d.BaseDefinition, owner, claimed, cfg.InstanceTTL, runner.run)
}
