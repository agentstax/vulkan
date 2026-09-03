package controller

import (
	"errors"

	"github.com/agentstax/vulkan/pkg/common/logging"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/produce/controller/datastore"
)

type ProduceController struct {
	Logger logging.Logger

	datastore *datastore.ProduceDatastore
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewProduceController(ds *iDatastore.PostgresDatastore, cfg *ControllerConfig) (*ProduceController, error) {
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

	produceDatastore, err := datastore.NewProduceDatastore(ds, &datastore.ProduceDatastoreConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &ProduceController{
		Logger:    cfg.Logger,
		datastore: produceDatastore,
	}, nil
}
