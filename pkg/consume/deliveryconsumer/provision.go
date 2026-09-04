package deliveryconsumer

import (
	"context"

	consumebase "github.com/agentstax/vulkan/pkg/consume/base"
	"github.com/agentstax/vulkan/pkg/worker"
	workercontroller "github.com/agentstax/vulkan/pkg/worker/controller"
)

// a nil Execution is a declined claim, not an error -- try again later.
func (d *DeliveryConsumerProvisioner[Message]) Provision(ctx context.Context, declared *worker.Worker) (worker.Execution, error) {
	parsed, err := workercontroller.ParseMetadata[deliveryConsumerMetadata](declared.Metadata)
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

	cfg := d.Config.withMetadata(ctx, parsed)
	resolvedTopic, err := d.GetTopic(ctx, declared.Owner.TopicId)
	if err != nil {
		return nil, err
	}

	base, err := consumebase.NewBaseConsumer(d.BaseProvisioner, declared.Owner, resolvedTopic, &consumebase.BaseConsumerConfig{
		TimeoutGrace:          cfg.TimeoutGrace,
		SlowDispatchThreshold: cfg.SlowDispatchThreshold,
	})
	if err != nil {
		return nil, err
	}

	runner, err := newDeliveryRunner(base, d.consumers, cfg)
	if err != nil {
		return nil, err
	}

	return consumebase.NewBaseInstance(d.BaseProvisioner, declared.Owner, claimed, cfg.InstanceTTL, runner.run)
}
