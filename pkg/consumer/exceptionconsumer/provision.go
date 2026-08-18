package exceptionconsumer

import (
	"context"

	"github.com/agentstax/vulkan/pkg/common"
	consumerbase "github.com/agentstax/vulkan/pkg/consumer/base"
	"github.com/agentstax/vulkan/pkg/worker"
	workercontroller "github.com/agentstax/vulkan/pkg/worker/controller"
)

// Declare creates this kind's worker row and writes the config onto it --
// the newest declaration wins.
func (f *ExceptionConsumerDefinition[Message]) Declare(ctx context.Context, owner *common.Owner) error {
	return f.DeclareWorker(ctx, owner, toExceptionConsumerMetadata(f.Config))
}

// a nil Execution is a declined claim, not an error -- try again later.
func (f *ExceptionConsumerDefinition[Message]) Provision(ctx context.Context, workerId int64, owner *common.Owner, metadata any) (worker.Execution, error) {
	parsed, err := workercontroller.ParseMetadata[exceptionConsumerMetadata](metadata)
	if err != nil {
		return nil, err
	}
	if err := parsed.Validate(); err != nil {
		return nil, err
	}
	claimed, err := f.RegisterInstance(ctx, workerId, owner, f.Config.InstanceTTL)
	if err != nil || claimed == nil {
		return nil, err
	}

	cfg := f.Config.withMetadata(ctx, parsed)
	resolvedTopic, err := f.GetTopic(ctx, owner.TopicId)
	if err != nil {
		return nil, err
	}
	base, err := consumerbase.NewBaseConsumer(f.BaseDefinition, owner, resolvedTopic, &consumerbase.BaseConsumerConfig{
		TimeoutGrace: cfg.TimeoutGrace,
		RecordMargin: cfg.RecordMargin,
	})
	if err != nil {
		return nil, err
	}
	runner, err := newExceptionRunner(base, f.consumers, cfg)
	if err != nil {
		return nil, err
	}
	return consumerbase.NewBaseExecution(f.BaseDefinition, owner, claimed, cfg.InstanceTTL, runner.run)
}
