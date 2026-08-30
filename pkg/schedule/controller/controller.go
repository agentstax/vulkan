package controller

import (
	"errors"

	"github.com/agentstax/vulkan/pkg/common/logging"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/schedule/controller/datastore"
)

type ScheduleController struct {
	Logger logging.Logger

	datastore *datastore.ScheduleDatastore
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewScheduleController(ds *iDatastore.PostgresDatastore, cfg *ControllerConfig) (*ScheduleController, error) {
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

	scheduleDatastore, err := datastore.NewScheduleDatastore(ds, &datastore.ScheduleDatastoreConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &ScheduleController{
		Logger:    cfg.Logger,
		datastore: scheduleDatastore,
	}, nil
}
