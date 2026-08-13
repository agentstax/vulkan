package compactionreadcost

import (
	"context"
	"errors"

	"github.com/agentstax/vulkan/pkg/alert"
	"github.com/agentstax/vulkan/pkg/alert/compactionreadcost/controller"
	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/consumer"
	consumercontroller "github.com/agentstax/vulkan/pkg/consumer/controller"
	"github.com/agentstax/vulkan/pkg/cron"
	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/producer"
	systemcontroller "github.com/agentstax/vulkan/pkg/system/controller"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
	"github.com/agentstax/vulkan/pkg/worker"
	workercontroller "github.com/agentstax/vulkan/pkg/worker/controller"
)

// CompactionReadCostDefinition is the alert's worker kind: one row owning the
// alert's consumer group on the job_requests topic.
type CompactionReadCostDefinition struct {
	Config *DefinitionConfig
	Logger logger.Logger

	ds                 *coredatastore.PostgresDatastore
	workers            *workercontroller.WorkerController
	topics             *topiccontroller.TopicController
	consumers          *consumercontroller.ConsumerController
	systems            *systemcontroller.SystemController
	controller         *controller.CompactionReadCostController
	alertProducer      *producer.Producer[alert.Alert]
	jobRequestConsumer *consumer.Consumer[cron.JobRequest]
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewCompactionReadCostDefinition(ds *coredatastore.PostgresDatastore, cfg *DefinitionConfig) (*CompactionReadCostDefinition, error) {
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}
	if cfg == nil {
		cfg = &DefinitionConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	workers, err := workercontroller.NewWorkerController(ds, &workercontroller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}
	topics, err := topiccontroller.NewTopicController(ds, &topiccontroller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}
	consumers, err := consumercontroller.NewConsumerController(ds, &consumercontroller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}
	systems, err := systemcontroller.NewSystemController(ds, &systemcontroller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}
	compactionReadCostController, err := controller.NewCompactionReadCostController(ds, &controller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}
	alertProducer, err := producer.NewProducer[alert.Alert](ds, &producer.ProducerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}
	jobRequestConsumer, err := consumer.NewConsumer[cron.JobRequest](ds, &consumer.ConsumerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &CompactionReadCostDefinition{
		Config:             cfg,
		Logger:             cfg.Logger,
		ds:                 ds,
		workers:            workers,
		topics:             topics,
		consumers:          consumers,
		systems:            systems,
		controller:         compactionReadCostController,
		alertProducer:      alertProducer,
		jobRequestConsumer: jobRequestConsumer,
	}, nil
}

func (d *CompactionReadCostDefinition) Name() string {
	return JobName
}

// Provision claims one live instance. nil = declined (target_instances
// already filled) -- not an error, try again later.
func (d *CompactionReadCostDefinition) Provision(ctx context.Context, workerId int64, owner *common.Owner, metadata any) (worker.Execution, error) {
	claimed, _, err := workercontroller.RegisterInstance[compactionReadCostMetadata](ctx, d.workers, workerId, owner, common.OwnerConsumerGroup, JobName, metadata, d.Config.InstanceTTL)
	if err != nil || claimed == nil {
		return nil, err
	}
	return newCompactionReadCostExecution(d, owner, claimed)
}
