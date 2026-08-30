package collector

import (
	"errors"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/common/logging"
	compactioncontroller "github.com/agentstax/vulkan/pkg/compaction/controller"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	metricscontroller "github.com/agentstax/vulkan/pkg/metrics/controller"
	"github.com/agentstax/vulkan/pkg/producer"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
	"github.com/agentstax/vulkan/pkg/worker"
	"github.com/agentstax/vulkan/pkg/worker/controller"
)

const WorkerMetricsCollector = "metrics_collector"

type MetricsCollectorProvisioner struct {
	Config *MetricsCollectorConfig
	Logger logging.Logger

	workers    *controller.WorkerController
	metrics    *metricscontroller.MetricsController
	topics     *topiccontroller.TopicController
	alertHeads *compactioncontroller.CompactionController
	producer   *producer.Producer // each Provision registers its own instance from it

	definition *worker.Definition
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewMetricsCollectorProvisioner(ds *iDatastore.PostgresDatastore, cfg *MetricsCollectorConfig) (*MetricsCollectorProvisioner, error) {
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

	alertHeads, err := compactioncontroller.NewCompactionController(ds, &compactioncontroller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	measurementProducer, err := producer.NewProducer(ds, &producer.ProducerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	definition, err := worker.NewDefinition(WorkerMetricsCollector, common.OwnerSystem, defaultMetricsCollectorMetadata())
	if err != nil {
		return nil, err
	}

	return &MetricsCollectorProvisioner{
		Config:     cfg,
		Logger:     cfg.Logger,
		workers:    workers,
		metrics:    metricsController,
		topics:     topics,
		alertHeads: alertHeads,
		producer:   measurementProducer,
		definition: definition,
	}, nil
}

func (d *MetricsCollectorProvisioner) Definition() *worker.Definition {
	return d.definition
}
