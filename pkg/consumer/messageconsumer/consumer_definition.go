package messageconsumer

// Package messageconsumer is ONE worker row of a consumer group: the loop
// that claims and processes fresh messages. consumer.NewConsumer assembles
// the full group; applications consume through it.
//
// Run alone:
//   - exception rows are written but never retried
//   - committed never advances, pinning retention
//   - the unresolved-exceptions alert eventually surfaces both

import (
	"context"

	consumerbase "github.com/agentstax/vulkan/pkg/consumer/base"
	"github.com/agentstax/vulkan/pkg/consumer/messageconsumer/controller"
	"github.com/agentstax/vulkan/pkg/datastore"
	metricsproducer "github.com/agentstax/vulkan/pkg/metrics/producer"
)

// setting this row's target_instances to 0 suspends just this kind's new
// claims, leaving the group's other consumer rows running
const WorkerMessageConsumer = "message_consumer"

type MessageConsumerDefinition[Message any] struct {
	Config *MessageConsumerConfig

	*consumerbase.BaseDefinition[Message]

	consumers *controller.MessageConsumerController
}

// NewMessageConsumerDefinition builds one worker row of the group, not the
// assembled consumer -- see the package doc.
// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewMessageConsumerDefinition[Message any](ds *datastore.PostgresDatastore, consumerFunc func(ctx context.Context, message *Message) error, abandonedEvents *metricsproducer.MetricsProducer, cfg *MessageConsumerConfig) (*MessageConsumerDefinition[Message], error) {
	if cfg == nil {
		cfg = &MessageConsumerConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	baseDefinition, err := consumerbase.NewBaseDefinition(ds, WorkerMessageConsumer, consumerFunc, abandonedEvents, &consumerbase.BaseDefinitionConfig{Logger: cfg.Logger, Retry: cfg.Retry})
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
