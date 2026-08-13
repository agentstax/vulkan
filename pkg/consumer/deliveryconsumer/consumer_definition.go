package deliveryconsumer

// The LIFECYCLE consumption path, PARKED: at the current feature set it is a
// strictly more expensive CURSOR (a delivery row per message vs one frontier)
// with no shipped capability CURSOR lacks. It re-earns its place only with the
// non-FIFO queue work (priority/delay/fairness). Keep its labs green; don't
// invest new work here.

import (
	"context"

	"github.com/agentstax/vulkan/pkg/common"
	consumerbase "github.com/agentstax/vulkan/pkg/consumer/base"
	"github.com/agentstax/vulkan/pkg/consumer/deliveryconsumer/controller"
	consumermetrics "github.com/agentstax/vulkan/pkg/consumer/metrics"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/worker"
)

// setting this row's target_instances to 0 suspends just this kind's new
// claims, leaving the group's other consumer rows running
const WorkerDeliveryConsumer = "delivery_consumer"

type DeliveryConsumerDefinition[Message any] struct {
	Config *DeliveryConsumerConfig

	*consumerbase.BaseDefinition[Message]

	consumers *controller.DeliveryConsumerController
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewDeliveryConsumerDefinition[Message any](ds *datastore.PostgresDatastore, consumerFunc func(ctx context.Context, message *Message) error, abandonedEvents *consumermetrics.MetricEventProducer, cfg *DeliveryConsumerConfig) (*DeliveryConsumerDefinition[Message], error) {
	if cfg == nil {
		cfg = &DeliveryConsumerConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	baseDefinition, err := consumerbase.NewBaseDefinition(ds, WorkerDeliveryConsumer, consumerFunc, abandonedEvents, cfg.Retry, cfg.Logger)
	if err != nil {
		return nil, err
	}
	consumers, err := controller.NewDeliveryConsumerController(ds, &controller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &DeliveryConsumerDefinition[Message]{
		Config:         cfg,
		BaseDefinition: baseDefinition,
		consumers:      consumers,
	}, nil
}

// a nil Execution is a declined claim, not an error -- try again later.
func (f *DeliveryConsumerDefinition[Message]) Provision(ctx context.Context, workerId int64, owner *common.Owner, metadata any) (worker.Execution, error) {
	claimed, err := f.RegisterInstance(ctx, workerId, owner, metadata, f.Config.InstanceTTL)
	if err != nil || claimed == nil {
		return nil, err
	}

	// ackMargin 0: it only feeds claimKeyedRun, and this path never claims keys
	base, err := consumerbase.NewBaseConsumer(ctx, f.BaseDefinition, owner, f.Config.TimeoutGrace, 0)
	if err != nil {
		return nil, err
	}
	runner, err := newDeliveryRunner(base, f.consumers, f.Config)
	if err != nil {
		return nil, err
	}
	return consumerbase.NewBaseExecution(f.BaseDefinition, owner, claimed, f.Config.InstanceTTL, runner.run)
}
