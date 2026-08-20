package controller

import (
	"errors"

	"github.com/agentstax/vulkan/pkg/common/logging"
	"github.com/agentstax/vulkan/pkg/consumergroup/cursoradvancer/controller/datastore"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
)

// CursorAdvancerController is the cursor advancer kind's door: the instance advances
// committed through it.
type CursorAdvancerController struct {
	Logger logging.Logger

	datastore *datastore.CursorAdvancerDatastore
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewCursorAdvancerController(ds *iDatastore.PostgresDatastore, cfg *ControllerConfig) (*CursorAdvancerController, error) {
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

	cursorAdvancerDatastore, err := datastore.NewCursorAdvancerDatastore(ds, &datastore.CursorAdvancerDatastoreConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &CursorAdvancerController{
		Logger:    cfg.Logger,
		datastore: cursorAdvancerDatastore,
	}, nil
}
