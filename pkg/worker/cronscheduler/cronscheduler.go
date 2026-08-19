package cronscheduler

import (
	"errors"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/cron"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/worker"
	"github.com/agentstax/vulkan/pkg/worker/controller"
	cronschedulercontroller "github.com/agentstax/vulkan/pkg/worker/cronscheduler/controller"
)

const WorkerCronScheduler = "cron_scheduler"

type CronSchedulerProvisioner struct {
	Config *CronSchedulerConfig
	Logger common.Logger

	ds         *iDatastore.PostgresDatastore
	workers    *controller.WorkerController
	controller *cronschedulercontroller.CronSchedulerController
	producer   *producer.Producer[cron.JobRequest] // each Provision registers its own instance from it

	definition *worker.Definition
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewCronSchedulerProvisioner(ds *iDatastore.PostgresDatastore, cfg *CronSchedulerConfig) (*CronSchedulerProvisioner, error) {
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}
	if cfg == nil {
		cfg = &CronSchedulerConfig{}
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

	schedulerController, err := cronschedulercontroller.NewCronSchedulerController(ds, &cronschedulercontroller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	jobProducer, err := producer.NewProducer[cron.JobRequest](ds, &producer.ProducerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	definition, err := worker.NewDefinition(WorkerCronScheduler, common.OwnerSystem, defaultCronSchedulerMetadata())
	if err != nil {
		return nil, err
	}

	return &CronSchedulerProvisioner{
		Config:     cfg,
		Logger:     cfg.Logger,
		ds:         ds,
		workers:    workers,
		controller: schedulerController,
		producer:   jobProducer,
		definition: definition,
	}, nil
}

func (d *CronSchedulerProvisioner) Definition() *worker.Definition {
	return d.definition
}
