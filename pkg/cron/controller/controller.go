package controller

import (
	"errors"

	"github.com/agentstax/vulkan/pkg/common/logging"
	"github.com/agentstax/vulkan/pkg/cron/controller/datastore"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
)

type CronJobController struct {
	Logger logging.Logger

	datastore *datastore.CronJobDatastore
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewCronJobController(ds *iDatastore.PostgresDatastore, cfg *ControllerConfig) (*CronJobController, error) {
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

	cronJobPayloadstore, err := datastore.NewCronJobDatastore(ds, &datastore.CronJobDatastoreConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &CronJobController{
		Logger:    cfg.Logger,
		datastore: cronJobPayloadstore,
	}, nil
}
