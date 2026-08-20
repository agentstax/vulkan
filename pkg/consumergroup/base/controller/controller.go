package controller

import (
	"errors"

	"github.com/agentstax/vulkan/pkg/common/logging"
	"github.com/agentstax/vulkan/pkg/consumergroup/base/controller/datastore"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
)

type KeyLeaseController struct {
	Logger logging.Logger

	datastore *datastore.KeyLeaseDatastore
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewKeyLeaseController(ds *iDatastore.PostgresDatastore, cfg *ControllerConfig) (*KeyLeaseController, error) {
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

	keyLeaseDatastore, err := datastore.NewKeyLeaseDatastore(ds, &datastore.KeyLeaseDatastoreConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &KeyLeaseController{
		Logger:    cfg.Logger,
		datastore: keyLeaseDatastore,
	}, nil
}
