package controller

import (
	"errors"

	"github.com/agentstax/vulkan/pkg/alert/partitioncount/controller/datastore"
	"github.com/agentstax/vulkan/pkg/common"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
)

const AlertPartitionCount = "partition_count"

type PartitionCountController struct {
	Logger common.Logger

	datastore *datastore.PartitionCountDatastore
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewPartitionCountController(ds *iDatastore.PostgresDatastore, cfg *ControllerConfig) (*PartitionCountController, error) {
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

	partitionCountDatastore, err := datastore.NewPartitionCountDatastore(ds, &datastore.PartitionCountDatastoreConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &PartitionCountController{
		Logger:    cfg.Logger,
		datastore: partitionCountDatastore,
	}, nil
}
