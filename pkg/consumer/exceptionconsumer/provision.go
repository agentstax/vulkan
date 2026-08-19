package exceptionconsumer

import (
	"context"

	consumerbase "github.com/agentstax/vulkan/pkg/consumer/base"
	"github.com/agentstax/vulkan/pkg/worker"
	workercontroller "github.com/agentstax/vulkan/pkg/worker/controller"
)

// a nil Execution is a declined claim, not an error -- try again later.
func (d *ExceptionConsumerProvisioner[Message]) Provision(ctx context.Context, declared *worker.Worker) (worker.Execution, error) {
	parsed, err := workercontroller.ParseMetadata[exceptionConsumerMetadata](declared.Metadata)
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

	base, err := consumerbase.NewBaseConsumer(d.BaseProvisioner, declared.Owner, resolvedTopic, &consumerbase.BaseConsumerConfig{
		TimeoutGrace: cfg.TimeoutGrace,
		RecordMargin: cfg.RecordMargin,
	})
	if err != nil {
		return nil, err
	}

	runner, err := newExceptionRunner(base, d.consumers, cfg)
	if err != nil {
		return nil, err
	}

	return consumerbase.NewBaseInstance(d.BaseProvisioner, declared.Owner, claimed, cfg.InstanceTTL, runner.run)
}
