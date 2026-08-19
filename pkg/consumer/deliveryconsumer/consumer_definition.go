package deliveryconsumer

// Package deliveryconsumer is ON HOLD -- prefer consumer.NewConsumer, which
// builds the cursor path. At the current feature set this path is a strictly
// more expensive cursor consumer (a delivery row per message vs one frontier)
// with no shipped capability the cursor path lacks; it re-earns its place only
// with the non-FIFO queue work (priority/delay/fairness). Not wired into
// consumer.NewConsumer -- reachable only by building a
// DeliveryConsumerDefinition directly. Keep its labs green; don't invest new
// work here.

import (
	"context"

	consumerbase "github.com/agentstax/vulkan/pkg/consumer/base"
	"github.com/agentstax/vulkan/pkg/consumer/deliveryconsumer/controller"
	"github.com/agentstax/vulkan/pkg/datastore"
	metricsproducer "github.com/agentstax/vulkan/pkg/metrics/producer"
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
func NewDeliveryConsumerDefinition[Message any](ds *datastore.PostgresDatastore, consumerFunc func(ctx context.Context, message *Message) error, abandonedEvents *metricsproducer.MetricsProducer, cfg *DeliveryConsumerConfig) (*DeliveryConsumerDefinition[Message], error) {
	if cfg == nil {
		cfg = &DeliveryConsumerConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	baseDefinition, err := consumerbase.NewBaseDefinition(ds, WorkerDeliveryConsumer, consumerFunc, abandonedEvents, &consumerbase.BaseDefinitionConfig{Logger: cfg.Logger, Retry: cfg.Retry})
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
