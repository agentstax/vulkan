package partitioncount

import (
	"errors"

	"github.com/agentstax/vulkan/pkg/alert"
	"github.com/agentstax/vulkan/pkg/alert/partitioncount/controller"
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

// PartitionCountProvisioner is the alert's worker kind: one row owning the
// alert's consumer group on the job_requests topic.
type PartitionCountProvisioner struct {
	Config *PartitionCountConfig
	Logger logging.Logger

	ds                  *iDatastore.PostgresDatastore
	workers             *workercontroller.WorkerController
	topics              *topiccontroller.TopicController
	consumers           *consumergroupcontroller.ConsumerGroupController
	controller          *controller.PartitionCountController
	alertProducer       *producer.Producer[alert.Alert]
	alertHeads          *compactioncontroller.CompactionController[alert.Alert]
	measurementProducer *producer.Producer[metrics.Measurement]
	jobRequestConsumer  *consumer.Consumer[cron.JobRequest]

	definition *worker.Definition
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewPartitionCountProvisioner(ds *iDatastore.PostgresDatastore, cfg *PartitionCountConfig) (*PartitionCountProvisioner, error) {
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}
	if cfg == nil {
		cfg = &PartitionCountConfig{}
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

	partitionCountController, err := controller.NewPartitionCountController(ds, &controller.ControllerConfig{
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

	definition, err := worker.NewDefinition(JobName, common.OwnerConsumerGroup, toPartitionCountMetadata(cfg))
	if err != nil {
		return nil, err
	}

	return &PartitionCountProvisioner{
		Config:              cfg,
		Logger:              cfg.Logger,
		ds:                  ds,
		workers:             workers,
		topics:              topics,
		consumers:           consumers,
		controller:          partitionCountController,
		alertProducer:       alertProducer,
		alertHeads:          alertHeads,
		measurementProducer: measurementProducer,
		jobRequestConsumer:  jobRequestConsumer,
		definition:          definition,
	}, nil
}

func (d *PartitionCountProvisioner) Definition() *worker.Definition {
	return d.definition
}
