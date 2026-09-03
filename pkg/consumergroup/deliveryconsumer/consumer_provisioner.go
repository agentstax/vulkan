package deliveryconsumer

// Package deliveryconsumer is ON HOLD -- prefer consumer.NewConsumer (the
// cursor path). ONE worker row, not an assembled group.
//   - a delivery row per message vs the cursor path's one frontier:
//     strictly more expensive, no shipped capability cursor lacks
//   - re-earns its place only with non-FIFO queue work
//     (priority/delay/fairness)
//   - not wired into consumer.NewConsumer -- reachable only by building a
//     DeliveryConsumerProvisioner directly
//   - keep its labs green; don't invest new work here

import (
	"context"

	"github.com/agentstax/vulkan/pkg/common"
	consumerbase "github.com/agentstax/vulkan/pkg/consumergroup/base"
	"github.com/agentstax/vulkan/pkg/consumergroup/deliveryconsumer/controller"
	"github.com/agentstax/vulkan/pkg/datastore"
	metricsproducer "github.com/agentstax/vulkan/pkg/metrics/producer"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/agentstax/vulkan/pkg/worker"
)

// setting this row's target_instances to 0 suspends just this kind's new
// claims, leaving the group's other consumer rows running
const WorkerDeliveryConsumer = "delivery_consumer"

type DeliveryConsumerProvisioner[Message topic.Versioned] struct {
	Config *DeliveryConsumerConfig

	*consumerbase.BaseProvisioner[Message]

	consumers *controller.DeliveryConsumerGroupController
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewDeliveryConsumerProvisioner[Message topic.Versioned](ds *datastore.PostgresDatastore, consumerFunc func(ctx context.Context, message *Message) error, schemaVersion int, metrics *metricsproducer.MetricsProducer, cfg *DeliveryConsumerConfig) (*DeliveryConsumerProvisioner[Message], error) {
	if cfg == nil {
		cfg = &DeliveryConsumerConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	definition, err := worker.NewDefinition(WorkerDeliveryConsumer, common.OwnerConsumerGroup, worker.NoInstanceTarget, toDeliveryConsumerMetadata(cfg))
	if err != nil {
		return nil, err
	}
	baseProvisioner, err := consumerbase.NewBaseProvisioner(ds, definition, consumerFunc, schemaVersion, metrics, &consumerbase.BaseProvisionerConfig{Logger: cfg.Logger, Retry: cfg.Retry})
	if err != nil {
		return nil, err
	}
	consumers, err := controller.NewDeliveryConsumerGroupController(ds, &controller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &DeliveryConsumerProvisioner[Message]{
		Config:          cfg,
		BaseProvisioner: baseProvisioner,
		consumers:       consumers,
	}, nil
}
