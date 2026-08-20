package cursoradvancer

import (
	"errors"

	"github.com/agentstax/vulkan/pkg/common"
	cursoradvancercontroller "github.com/agentstax/vulkan/pkg/consumergroup/cursoradvancer/controller"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/worker"
	"github.com/agentstax/vulkan/pkg/worker/controller"
)

const WorkerCursorAdvancer = "cursor_advancer"

type CursorAdvancerProvisioner struct {
	Config *CursorAdvancerConfig
	Logger common.Logger

	workers    *controller.WorkerController
	controller *cursoradvancercontroller.CursorAdvancerController

	definition *worker.Definition
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewCursorAdvancerProvisioner(ds *iDatastore.PostgresDatastore, cfg *CursorAdvancerConfig) (*CursorAdvancerProvisioner, error) {
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

	definition, err := worker.NewDefinition(WorkerCursorAdvancer, common.OwnerConsumerGroup, defaultCursorAdvancerMetadata())
	if err != nil {
		return nil, err
	}

	return &CursorAdvancerProvisioner{
		Config:     cfg,
		Logger:     cfg.Logger,
		workers:    workers,
		controller: advanceController,
		definition: definition,
	}, nil
}

func (d *CursorAdvancerProvisioner) Definition() *worker.Definition {
	return d.definition
}
