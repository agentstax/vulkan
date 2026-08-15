package cronscheduler

import (
	"context"
	"errors"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/cron"
	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/worker"
	"github.com/agentstax/vulkan/pkg/worker/controller"
	"github.com/agentstax/vulkan/pkg/worker/cronscheduler/datastore"
)

const WorkerCronScheduler = "cron_scheduler"

type CronSchedulerDefinition struct {
	Config *CronSchedulerConfig
	Logger logger.Logger

	workers   *controller.WorkerController
	datastore *datastore.CronSchedulerDatastore
	producer  *producer.Producer[cron.JobRequest] // each execution's Run registers its own instance from it
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
		workers:   workers,
		datastore: schedulerDatastore,
		producer:  jobProducer,
	}, nil
}

func (s *CronSchedulerDefinition) Name() string {
	return WorkerCronScheduler
}

// Register claims one live instance. nil = declined (target_instances
// already filled) -- not an error, try again later.
func (s *CronSchedulerDefinition) Provision(ctx context.Context, workerId int64, owner *common.Owner, metadata any) (worker.Execution, error) {
	parsed, err := controller.ParseMetadata[cronSchedulerMetadata](metadata)
	if err != nil {
		return nil, err
	}
	if err := parsed.Validate(); err != nil {
		return nil, err
	}
	claimed, err := controller.RegisterInstance(ctx, s.workers, workerId, owner, common.OwnerSystem, WorkerCronScheduler, s.Config.InstanceTTL)
	if err != nil || claimed == nil {
		return nil, err
	}
	return newCronSchedulerExecution(s, owner, claimed, parsed)
}
