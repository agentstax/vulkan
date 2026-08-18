package waterline

import (
	"errors"

	"github.com/agentstax/vulkan/pkg/common"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/worker/controller"
	"github.com/agentstax/vulkan/pkg/worker/waterline/datastore"
)

const WorkerWaterline = "waterline"

type WaterlineDefinition struct {
	Config *WaterlineConfig
	Logger common.Logger

	workers   *controller.WorkerController
	datastore *datastore.WaterlineDatastore
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewWaterlineDefinition(ds *iDatastore.PostgresDatastore, cfg *WaterlineConfig) (*WaterlineDefinition, error) {
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}
	if cfg == nil {
		cfg = &WaterlineConfig{}
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

	waterlineDatastore, err := datastore.NewWaterlineDatastore(ds, &datastore.WaterlineDatastoreConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &WaterlineDefinition{
		Config:    cfg,
		Logger:    cfg.Logger,
		workers:   workers,
		datastore: waterlineDatastore,
	}, nil
}

func (w *WaterlineDefinition) Name() string {
	return WorkerWaterline
}
