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

	"github.com/agentstax/vulkan/pkg/common"
	consumerbase "github.com/agentstax/vulkan/pkg/consumergroup/base"
	"github.com/agentstax/vulkan/pkg/consumergroup/messageconsumer/controller"
	"github.com/agentstax/vulkan/pkg/datastore"
	metricsproducer "github.com/agentstax/vulkan/pkg/metrics/producer"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/agentstax/vulkan/pkg/worker"
)

// setting this row's target_instances to 0 suspends just this kind's new
// claims, leaving the group's other consumer rows running
const WorkerMessageConsumer = "message_consumer"

type MessageConsumerProvisioner[Message any] struct {
	Config *MessageConsumerConfig

	*consumerbase.BaseProvisioner[Message]

	consumers *controller.MessageConsumerGroupController
}

// NewMessageConsumerProvisioner builds one worker row of the group, not the
// assembled consumer -- see the package doc.
// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewMessageConsumerProvisioner[Message any](ds *datastore.PostgresDatastore, consumerFunc func(ctx context.Context, message *Message) error, schemaVersion topic.SchemaVersion, metrics *metricsproducer.MetricsProducer, cfg *MessageConsumerConfig) (*MessageConsumerProvisioner[Message], error) {
	if cfg == nil {
		cfg = &MessageConsumerConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	definition, err := worker.NewDefinition(WorkerMessageConsumer, common.OwnerConsumerGroup, toMessageConsumerMetadata(cfg))
	if err != nil {
		return nil, err
	}
	baseProvisioner, err := consumerbase.NewBaseProvisioner(ds, definition, consumerFunc, schemaVersion, metrics, &consumerbase.BaseProvisionerConfig{Logger: cfg.Logger, Retry: cfg.Retry})
	if err != nil {
		return nil, err
	}
	consumers, err := controller.NewMessageConsumerGroupController(ds, &controller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &MessageConsumerProvisioner[Message]{
		Config:          cfg,
		BaseProvisioner: baseProvisioner,
		consumers:       consumers,
	}, nil
}
