package compactionreadcost

import (
	"errors"

	"github.com/agentstax/vulkan/pkg/alert"
	"github.com/agentstax/vulkan/pkg/alert/compactionreadcost/controller"
	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/common/logging"
	compactioncontroller "github.com/agentstax/vulkan/pkg/compaction/controller"
	"github.com/agentstax/vulkan/pkg/consumer"
	consumergroupcontroller "github.com/agentstax/vulkan/pkg/consumergroup/controller"
	"github.com/agentstax/vulkan/pkg/cron"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/metrics"
	"github.com/agentstax/vulkan/pkg/producer"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
	"github.com/agentstax/vulkan/pkg/worker"
	workercontroller "github.com/agentstax/vulkan/pkg/worker/controller"
)

// CompactionReadCostProvisioner is the alert's worker kind: one row owning the
// alert's consumer group on the job_requests topic.
type CompactionReadCostProvisioner struct {
	Config *CompactionReadCostConfig
	Logger logging.Logger

	ds                  *iDatastore.PostgresDatastore
	workers             *workercontroller.WorkerController
	topics              *topiccontroller.TopicController
	consumers           *consumergroupcontroller.ConsumerGroupController
	controller          *controller.CompactionReadCostController
	alertProducer       *producer.Producer[alert.Alert]
	alertHeads          *compactioncontroller.CompactionController[alert.Alert]
	measurementProducer *producer.Producer[metrics.Measurement]
	jobRequestConsumer  *consumer.Consumer[cron.JobRequest]

	definition *worker.Definition
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewCompactionReadCostProvisioner(ds *iDatastore.PostgresDatastore, cfg *CompactionReadCostConfig) (*CompactionReadCostProvisioner, error) {
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}
	if cfg == nil {
		cfg = &CompactionReadCostConfig{}
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

	consumers, err := consumergroupcontroller.NewConsumerGroupController(ds, &consumergroupcontroller.ControllerConfig{
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

	alertHeads, err := compactioncontroller.NewCompactionController[alert.Alert](ds, &compactioncontroller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	measurementProducer, err := producer.NewProducer[metrics.Measurement](ds, &producer.ProducerConfig{
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

	definition, err := worker.NewDefinition(JobName, common.OwnerConsumerGroup, toCompactionReadCostMetadata(cfg))
	if err != nil {
		return nil, err
	}

	return &CompactionReadCostProvisioner{
		Config:              cfg,
		Logger:              cfg.Logger,
		ds:                  ds,
		workers:             workers,
		topics:              topics,
		consumers:           consumers,
		controller:          compactionReadCostController,
		alertProducer:       alertProducer,
		alertHeads:          alertHeads,
		measurementProducer: measurementProducer,
		jobRequestConsumer:  jobRequestConsumer,
		definition:          definition,
	}, nil
}

func (d *CompactionReadCostProvisioner) Definition() *worker.Definition {
	return d.definition
}
