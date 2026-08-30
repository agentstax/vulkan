package producer

import (
	"errors"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/common/logging"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer"
	scheduleproducercontroller "github.com/agentstax/vulkan/pkg/schedule/producer/controller"
	"github.com/agentstax/vulkan/pkg/worker"
	"github.com/agentstax/vulkan/pkg/worker/controller"
)

const WorkerScheduleProducer = "schedule_producer"

type ScheduleProducerProvisioner struct {
	Config *ScheduleProducerConfig
	Logger logging.Logger

	ds         *iDatastore.PostgresDatastore
	workers    *controller.WorkerController
	controller *scheduleproducercontroller.ScheduleProducerController
	producer   *producer.Producer // each Provision registers its own instance from it

	definition *worker.Definition
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewScheduleProducerProvisioner(ds *iDatastore.PostgresDatastore, cfg *ScheduleProducerConfig) (*ScheduleProducerProvisioner, error) {
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}
	if cfg == nil {
		cfg = &ScheduleProducerConfig{}
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

	schedulerController, err := scheduleproducercontroller.NewScheduleProducerController(ds, &scheduleproducercontroller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	jobProducer, err := producer.NewProducer(ds, &producer.ProducerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	definition, err := worker.NewDefinition(WorkerScheduleProducer, common.OwnerSystem, defaultScheduleProducerMetadata())
	if err != nil {
		return nil, err
	}

	return &ScheduleProducerProvisioner{
		Config:     cfg,
		Logger:     cfg.Logger,
		ds:         ds,
		workers:    workers,
		controller: schedulerController,
		producer:   jobProducer,
		definition: definition,
	}, nil
}

func (d *ScheduleProducerProvisioner) Definition() *worker.Definition {
	return d.definition
}
