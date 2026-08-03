package controller

import (
	"errors"

	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/worker/controller/datastore"
)

type WorkerController struct {
	Logger logger.Logger // copied from Config.Logger at construction

	datastore *datastore.WorkerDatastore
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewWorkerController(ds *coredatastore.PostgresDatastore, cfg *ControllerConfig) (*WorkerController, error) {
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

	workerDatastore, err := datastore.NewWorkerDatastore(ds, &datastore.WorkerDatastoreConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &WorkerController{
		Logger:    cfg.Logger,
		datastore: workerDatastore,
	}, nil
}
