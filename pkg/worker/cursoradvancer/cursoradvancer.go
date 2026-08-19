package cursoradvancer

import (
	"errors"

	"github.com/agentstax/vulkan/pkg/common"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/worker/controller"
	cursoradvancercontroller "github.com/agentstax/vulkan/pkg/worker/cursoradvancer/controller"
)

const WorkerCursorAdvancer = "cursor_advancer"

type CursorAdvancerDefinition struct {
	Config *CursorAdvancerConfig
	Logger common.Logger

	workers    *controller.WorkerController
	controller *cursoradvancercontroller.CursorAdvancerController
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewCursorAdvancerDefinition(ds *iDatastore.PostgresDatastore, cfg *CursorAdvancerConfig) (*CursorAdvancerDefinition, error) {
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}
	if cfg == nil {
		cfg = &CursorAdvancerConfig{}
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

	advanceController, err := cursoradvancercontroller.NewCursorAdvancerController(ds, &cursoradvancercontroller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &CursorAdvancerDefinition{
		Config:     cfg,
		Logger:     cfg.Logger,
		workers:    workers,
		controller: advanceController,
	}, nil
}

func (d *CursorAdvancerDefinition) Name() string {
	return WorkerCursorAdvancer
}
