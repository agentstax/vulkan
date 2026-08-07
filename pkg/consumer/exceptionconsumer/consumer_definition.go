package exceptionconsumer

import (
	"context"

	"github.com/agentstax/vulkan/pkg/common"
	consumerbase "github.com/agentstax/vulkan/pkg/consumer/base"
	"github.com/agentstax/vulkan/pkg/consumer/exceptionconsumer/controller"
	consumermetrics "github.com/agentstax/vulkan/pkg/consumer/metrics"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/worker"
)

// setting this row's target_instances to 0 suspends just this kind's new
// claims, leaving the group's other consumer rows running
const WorkerExceptionConsumer = "exception_consumer"

type ExceptionConsumerDefinition[Message any] struct {
	Config *ExceptionConsumerConfig

	*consumerbase.BaseDefinition[Message]

	consumers *controller.ExceptionConsumerController
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewExceptionConsumerDefinition[Message any](ds *datastore.PostgresDatastore, consumerFunc func(ctx context.Context, message *Message) error, abandonedEvents *consumermetrics.MetricEventProducer, cfg *ExceptionConsumerConfig) (*ExceptionConsumerDefinition[Message], error) {
	if cfg == nil {
		cfg = &ExceptionConsumerConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	baseDefinition, err := consumerbase.NewBaseDefinition(ds, WorkerExceptionConsumer, consumerFunc, abandonedEvents, cfg.Retry, cfg.Logger)
	if err != nil {
		return nil, err
	}
	consumers, err := controller.NewExceptionConsumerController(ds, &controller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &ExceptionConsumerDefinition[Message]{
		Config:         cfg,
		BaseDefinition: baseDefinition,
		consumers:      consumers,
	}, nil
}

// a nil Execution is a declined claim, not an error -- try again later.
func (f *ExceptionConsumerDefinition[Message]) Provision(ctx context.Context, workerId int64, owner *common.Owner, metadata any) (worker.Execution, error) {
	claimed, err := f.RegisterInstance(ctx, workerId, owner, metadata, f.Config.InstanceTTL)
	if err != nil || claimed == nil {
		return nil, err
	}

	base, err := consumerbase.NewBaseConsumer(ctx, f.BaseDefinition, owner, f.Config.TimeoutGrace, f.Config.AckMargin)
	if err != nil {
		return nil, err
	}
	runner, err := newExceptionRunner(base, f.consumers, f.Config)
	if err != nil {
		return nil, err
	}
	return consumerbase.NewBaseExecution(f.BaseDefinition, owner, claimed, f.Config.InstanceTTL, runner.run)
}
