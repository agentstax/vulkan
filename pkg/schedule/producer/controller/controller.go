package controller

import (
	"errors"

	"github.com/agentstax/vulkan/pkg/common/logging"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/schedule/producer/controller/datastore"
)

// ScheduleProducerController is the schedule producer kind's only path to
// persistence: the instance scans, claims, and advances schedule rows
// through it.
type ScheduleProducerController struct {
	Logger logging.Logger

	datastore *datastore.ScheduleProducerDatastore
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewScheduleProducerController(ds *iDatastore.PostgresDatastore, cfg *ControllerConfig) (*ScheduleProducerController, error) {
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

	schedulerDatastore, err := datastore.NewScheduleProducerDatastore(ds, &datastore.ScheduleProducerDatastoreConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &ScheduleProducerController{
		Logger:    cfg.Logger,
		datastore: schedulerDatastore,
	}, nil
}
