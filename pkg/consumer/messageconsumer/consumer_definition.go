package messageconsumer

import (
	"context"

	"github.com/agentstax/vulkan/pkg/common"
	consumerbase "github.com/agentstax/vulkan/pkg/consumer/base"
	"github.com/agentstax/vulkan/pkg/consumer/messageconsumer/controller"
	consumermetrics "github.com/agentstax/vulkan/pkg/consumer/metrics"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/worker"
	workercontroller "github.com/agentstax/vulkan/pkg/worker/controller"
)

// setting this row's target_instances to 0 suspends just this kind's new
// claims, leaving the group's other consumer rows running
const WorkerMessageConsumer = "message_consumer"

type MessageConsumerDefinition[Message any] struct {
	Config *MessageConsumerConfig

	*consumerbase.BaseDefinition[Message]

	consumers *controller.MessageConsumerController
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewMessageConsumerDefinition[Message any](ds *datastore.PostgresDatastore, consumerFunc func(ctx context.Context, message *Message) error, abandonedEvents *consumermetrics.MetricEventProducer, cfg *MessageConsumerConfig) (*MessageConsumerDefinition[Message], error) {
	if cfg == nil {
		cfg = &MessageConsumerConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	baseDefinition, err := consumerbase.NewBaseDefinition(ds, WorkerMessageConsumer, consumerFunc, abandonedEvents, cfg.Retry, cfg.Logger)
	if err != nil {
		return nil, err
	}
	consumers, err := controller.NewMessageConsumerController(ds, &controller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &MessageConsumerDefinition[Message]{
		Config:         cfg,
		BaseDefinition: baseDefinition,
		consumers:      consumers,
	}, nil
}

// Declare creates this kind's worker row and refreshes its metadata defaults
// from the config; operator overrides survive.
func (f *MessageConsumerDefinition[Message]) Declare(ctx context.Context, owner *common.Owner) error {
	return f.DeclareWorker(ctx, owner, toMessageConsumerMetadata(f.Config))
}

// a nil Execution is a declined claim, not an error -- try again later.
func (f *MessageConsumerDefinition[Message]) Provision(ctx context.Context, workerId int64, owner *common.Owner, metadata any) (worker.Execution, error) {
	parsed, err := workercontroller.ParseMetadata[messageConsumerMetadata](metadata)
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
	base, err := consumerbase.NewBaseConsumer(ctx, f.BaseDefinition, owner, cfg.TimeoutGrace, cfg.AckMargin)
	if err != nil {
		return nil, err
	}
	runner, err := newMessageRunner(base, f.consumers, cfg)
	if err != nil {
		return nil, err
	}
	return consumerbase.NewBaseExecution(f.BaseDefinition, owner, claimed, cfg.InstanceTTL, runner.run)
}
