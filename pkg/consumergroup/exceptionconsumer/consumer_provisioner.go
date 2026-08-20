package exceptionconsumer

// Package exceptionconsumer is ONE worker row of a consumer group: the
// loop that retries the exception rows the message consumer wrote.
// consumer.NewConsumer assembles the full group; applications consume
// through it.
//
// Run alone:
//   - no fresh messages are claimed
//   - committed never advances

import (
	"context"

	"github.com/agentstax/vulkan/pkg/common"
	consumerbase "github.com/agentstax/vulkan/pkg/consumergroup/base"
	"github.com/agentstax/vulkan/pkg/consumergroup/exceptionconsumer/controller"
	"github.com/agentstax/vulkan/pkg/datastore"
	metricsproducer "github.com/agentstax/vulkan/pkg/metrics/producer"
	"github.com/agentstax/vulkan/pkg/worker"
)

// setting this row's target_instances to 0 suspends just this kind's new
// claims, leaving the group's other consumer rows running
const WorkerExceptionConsumer = "exception_consumer"

type ExceptionConsumerProvisioner[Message any] struct {
	Config *ExceptionConsumerConfig

	*consumerbase.BaseProvisioner[Message]

	consumers *controller.ExceptionConsumerGroupController
}

// NewExceptionConsumerProvisioner builds one worker row of the group, not
// the assembled consumer -- see the package doc.
// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewExceptionConsumerProvisioner[Message any](ds *datastore.PostgresDatastore, consumerFunc func(ctx context.Context, message *Message) error, abandonedEvents *metricsproducer.MetricsProducer, cfg *ExceptionConsumerConfig) (*ExceptionConsumerProvisioner[Message], error) {
	if cfg == nil {
		cfg = &ExceptionConsumerConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	definition, err := worker.NewDefinition(WorkerExceptionConsumer, common.OwnerConsumerGroup, toExceptionConsumerMetadata(cfg))
	if err != nil {
		return nil, err
	}
	baseProvisioner, err := consumerbase.NewBaseProvisioner(ds, definition, consumerFunc, abandonedEvents, &consumerbase.BaseProvisionerConfig{Logger: cfg.Logger, Retry: cfg.Retry})
	if err != nil {
		return nil, err
	}
	consumers, err := controller.NewExceptionConsumerGroupController(ds, &controller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &ExceptionConsumerProvisioner[Message]{
		Config:          cfg,
		BaseProvisioner: baseProvisioner,
		consumers:       consumers,
	}, nil
}
