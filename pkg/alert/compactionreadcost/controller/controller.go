package controller

import (
	"errors"

	"github.com/agentstax/vulkan/pkg/alert/compactionreadcost/controller/datastore"
	"github.com/agentstax/vulkan/pkg/common/logging"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
)

type CompactionReadCostController struct {
	Logger logging.Logger

	datastore *datastore.CompactionReadCostDatastore
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewCompactionReadCostController(ds *iDatastore.PostgresDatastore, cfg *ControllerConfig) (*CompactionReadCostController, error) {
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

	compactionReadCostDatastore, err := datastore.NewCompactionReadCostDatastore(ds, &datastore.CompactionReadCostDatastoreConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &CompactionReadCostController{
		Logger:    cfg.Logger,
		datastore: compactionReadCostDatastore,
	}, nil
}
