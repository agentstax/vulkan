package metricscollector

import (
	"errors"

	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/metrics"
	metricscontroller "github.com/agentstax/vulkan/pkg/metrics/controller"
	"github.com/agentstax/vulkan/pkg/producer"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
	"github.com/agentstax/vulkan/pkg/worker/controller"
)

const WorkerMetricsCollector = "metrics_collector"

type MetricsCollectorDefinition struct {
	Config *MetricsCollectorConfig
	Logger logger.Logger

	workers  *controller.WorkerController
	metrics  *metricscontroller.MetricsController
	topics   *topiccontroller.TopicController
	producer *producer.Producer[metrics.Sample] // each Provision registers its own instance from it
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewMetricsCollectorDefinition(ds *coredatastore.PostgresDatastore, cfg *MetricsCollectorConfig) (*MetricsCollectorDefinition, error) {
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}
	if cfg == nil {
		cfg = &MetricsCollectorConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	workers, err := controller.NewWorkerController(ds, &controller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	metricsController, err := metricscontroller.NewMetricsController(ds, &metricscontroller.ControllerConfig{
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

	sampleProducer, err := producer.NewProducer[metrics.Sample](ds, &producer.ProducerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &MetricsCollectorDefinition{
		Config:   cfg,
		Logger:   cfg.Logger,
		workers:  workers,
		metrics:  metricsController,
		topics:   topics,
		producer: sampleProducer,
	}, nil
}

func (c *MetricsCollectorDefinition) Name() string {
	return WorkerMetricsCollector
}
