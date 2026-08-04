package controller

import (
	"errors"
	"fmt"

	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/system/controller/datastore"
	"github.com/agentstax/vulkan/pkg/worker"
)

type SystemController struct {
	Logger logger.Logger

	declarers []worker.Declarer
	datastore *datastore.SystemDatastore
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range. declarers run on every
// RegisterSystem to create the system's worker rows -- pass them only from a
// registrar; a controller built for reads needs none.
func NewSystemController(ds *coredatastore.PostgresDatastore, cfg *ControllerConfig, declarers ...worker.Declarer) (*SystemController, error) {
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}
	for i, declarer := range declarers {
		if declarer == nil {
			return nil, fmt.Errorf("declarer %d must not be nil", i)
		}
	}
	if cfg == nil {
		cfg = &ControllerConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	systemDatastore, err := datastore.NewSystemDatastore(ds, &datastore.SystemDatastoreConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &SystemController{
		Logger:    cfg.Logger,
		declarers: declarers,
		datastore: systemDatastore,
	}, nil
}
