package controller

import (
	"errors"

	"github.com/agentstax/vulkan/pkg/common/logging"
	"github.com/agentstax/vulkan/pkg/consume/controller/datastore"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
)

type ConsumeController struct {
	Logger logging.Logger

	datastore *datastore.ConsumeDatastore
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewConsumeController(ds *iDatastore.PostgresDatastore, cfg *ControllerConfig) (*ConsumeController, error) {
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

	consumerDatastore, err := datastore.NewConsumeDatastore(ds, &datastore.ConsumeDatastoreConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &ConsumeController{
		Logger:    cfg.Logger,
		datastore: consumerDatastore,
	}, nil
}
