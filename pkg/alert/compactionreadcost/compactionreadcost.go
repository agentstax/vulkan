package compactionreadcost

import (
	"errors"

	"github.com/agentstax/vulkan/pkg/alert"
	"github.com/agentstax/vulkan/pkg/alert/compactionreadcost/controller"
	"github.com/agentstax/vulkan/pkg/common"
	compactioncontroller "github.com/agentstax/vulkan/pkg/compaction/controller"
	"github.com/agentstax/vulkan/pkg/consumer"
	consumercontroller "github.com/agentstax/vulkan/pkg/consumer/controller"
	"github.com/agentstax/vulkan/pkg/cron"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/metrics"
	"github.com/agentstax/vulkan/pkg/producer"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
	workercontroller "github.com/agentstax/vulkan/pkg/worker/controller"
)

// CompactionReadCostDefinition is the alert's worker kind: one row owning the
// alert's consumer group on the job_requests topic.
type CompactionReadCostDefinition struct {
	Config *CompactionReadCostConfig
	Logger common.Logger

	ds                  *iDatastore.PostgresDatastore
	workers             *workercontroller.WorkerController
	topics              *topiccontroller.TopicController
	consumers           *consumercontroller.ConsumerController
	controller          *controller.CompactionReadCostController
	alertProducer       *producer.Producer[alert.Alert]
	alertHeads          *compactioncontroller.CompactionController[alert.Alert]
	measurementProducer *producer.Producer[metrics.Measurement]
	jobRequestConsumer  *consumer.Consumer[cron.JobRequest]
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewCompactionReadCostDefinition(ds *iDatastore.PostgresDatastore, cfg *CompactionReadCostConfig) (*CompactionReadCostDefinition, error) {
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
	consumers, err := consumercontroller.NewConsumerController(ds, &consumercontroller.ControllerConfig{
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

	return &CompactionReadCostDefinition{
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
	}, nil
}

func (d *CompactionReadCostDefinition) Name() string {
	return JobName
}
