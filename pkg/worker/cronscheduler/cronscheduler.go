package cronscheduler

import (
	"context"
	"errors"

	"github.com/agentstax/vulkan/pkg/common"
	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/worker"
	"github.com/agentstax/vulkan/pkg/worker/controller"
	"github.com/agentstax/vulkan/pkg/worker/cronscheduler/datastore"
)

const WorkerCronScheduler = "cron_scheduler"

type CronSchedulerDefinition struct {
	Config *CronSchedulerConfig
	Logger logger.Logger

	ds        *coredatastore.PostgresDatastore // each instance constructs its own JobRequest producer from it
	workers   *controller.WorkerController
	datastore *datastore.CronSchedulerDatastore
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

	return &CronSchedulerDefinition{
		Config:    cfg,
		Logger:    cfg.Logger,
		ds:        ds,
		workers:   workers,
		datastore: schedulerDatastore,
	}, nil
}

func (s *CronSchedulerDefinition) Name() string {
	return WorkerCronScheduler
}

// Register claims one live instance. nil = declined (target_instances
// already filled) -- not an error, try again later.
func (s *CronSchedulerDefinition) Provision(ctx context.Context, workerId int64, owner *common.Owner, metadata any) (worker.Execution, error) {
	claimed, parsed, err := controller.RegisterInstance[cronSchedulerMetadata](ctx, s.workers, workerId, owner, common.OwnerSystem, WorkerCronScheduler, metadata, s.Config.InstanceTTL)
	if err != nil || claimed == nil {
		return nil, err
	}
	return newCronSchedulerExecution(s, owner, claimed, parsed)
}
