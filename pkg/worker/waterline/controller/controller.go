package controller

import (
	"errors"

	"github.com/agentstax/vulkan/pkg/common"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/worker/waterline/controller/datastore"
)

// WaterlineController is the waterline kind's door: the execution advances
// committed through it.
type WaterlineController struct {
	Logger common.Logger

	datastore *datastore.WaterlineDatastore
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewWaterlineController(ds *iDatastore.PostgresDatastore, cfg *ControllerConfig) (*WaterlineController, error) {
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

	waterlineDatastore, err := datastore.NewWaterlineDatastore(ds, &datastore.WaterlineDatastoreConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &WaterlineController{
		Logger:    cfg.Logger,
		datastore: waterlineDatastore,
	}, nil
}
