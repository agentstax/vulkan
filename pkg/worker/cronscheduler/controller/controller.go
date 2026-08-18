package controller

import (
	"errors"

	"github.com/agentstax/vulkan/pkg/common"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/worker/cronscheduler/controller/datastore"
)

// CronSchedulerController is the cron scheduler kind's door: the execution
// scans, claims, and advances cron_job rows through it.
type CronSchedulerController struct {
	Logger common.Logger

	datastore *datastore.CronSchedulerDatastore
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewCronSchedulerController(ds *iDatastore.PostgresDatastore, cfg *ControllerConfig) (*CronSchedulerController, error) {
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}
	if cfg == nil {
		cfg = &ControllerConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	schedulerDatastore, err := datastore.NewCronSchedulerDatastore(ds, &datastore.CronSchedulerDatastoreConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &CronSchedulerController{
		Logger:    cfg.Logger,
		datastore: schedulerDatastore,
	}, nil
}
