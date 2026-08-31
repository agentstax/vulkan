package messageconsumer

import (
	"context"

	consumerbase "github.com/agentstax/vulkan/pkg/consumergroup/base"
	"github.com/agentstax/vulkan/pkg/worker"
	workercontroller "github.com/agentstax/vulkan/pkg/worker/controller"
)

// a nil Execution is a declined claim, not an error -- try again later.
func (d *MessageConsumerProvisioner[Message]) Provision(ctx context.Context, declared *worker.WorkerData) (worker.Execution, error) {
	parsed, err := workercontroller.ParseMetadata[MessageConsumerMetadata](declared.Metadata)
	if err != nil {
		return nil, err
	}
	if err := parsed.Validate(); err != nil {
		return nil, err
	}
	claimed, err := d.RegisterInstance(ctx, declared.Id, declared.Owner, d.Config.InstanceTTL)
	if err != nil || claimed == nil {
		return nil, err
	}

	cfg := d.Config.withMetadata(parsed)
	resolvedTopic, err := d.GetTopic(ctx, declared.Owner.TopicId)
	if err != nil {
		return nil, err
	}

	base, err := consumerbase.NewBaseConsumer(d.BaseProvisioner, declared.Owner, resolvedTopic, &consumerbase.BaseConsumerConfig{
		TimeoutGrace:          cfg.TimeoutGrace,
		RecordMargin:          cfg.RecordMargin,
		SlowDispatchThreshold: cfg.SlowDispatchThreshold,
	})
	if err != nil {
		return nil, err
	}

	runner, err := newMessageRunner(base, d.consumers, cfg)
	if err != nil {
		return nil, err
	}

	return consumerbase.NewBaseInstance(d.BaseProvisioner, declared.Owner, claimed, cfg.InstanceTTL, runner.run)
}
