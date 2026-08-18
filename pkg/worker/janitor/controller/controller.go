package controller

import (
	"errors"

	"github.com/agentstax/vulkan/pkg/common"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/worker/janitor/controller/datastore"
)

// JanitorController is the janitor kind's door: the execution's sweep pass
// drops and drains expired storage through it.
type JanitorController struct {
	Logger common.Logger

	datastore *datastore.JanitorDatastore
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewJanitorController(ds *iDatastore.PostgresDatastore, cfg *ControllerConfig) (*JanitorController, error) {
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

	janitorDatastore, err := datastore.NewJanitorDatastore(ds, &datastore.JanitorDatastoreConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &JanitorController{
		Logger:    cfg.Logger,
		datastore: janitorDatastore,
	}, nil
}
