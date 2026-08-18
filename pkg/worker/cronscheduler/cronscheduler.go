package cronscheduler

import (
	"errors"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/cron"
	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/worker/controller"
	"github.com/agentstax/vulkan/pkg/worker/cronscheduler/datastore"
)

const WorkerCronScheduler = "cron_scheduler"

type CronSchedulerDefinition struct {
	Config *CronSchedulerConfig
	Logger common.Logger

	ds        *coredatastore.PostgresDatastore
	workers   *controller.WorkerController
	datastore *datastore.CronSchedulerDatastore
	producer  *producer.Producer[cron.JobRequest] // each Provision registers its own instance from it
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewCronSchedulerDefinition(ds *coredatastore.PostgresDatastore, cfg *CronSchedulerConfig) (*CronSchedulerDefinition, error) {
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

	schedulerDatastore, err := datastore.NewCronSchedulerDatastore(ds, &datastore.CronSchedulerDatastoreConfig{
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

	return &CronSchedulerDefinition{
		Config:    cfg,
		Logger:    cfg.Logger,
		ds:        ds,
		workers:   workers,
		datastore: schedulerDatastore,
		producer:  jobProducer,
	}, nil
}

func (s *CronSchedulerDefinition) Name() string {
	return WorkerCronScheduler
}
