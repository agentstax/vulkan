package controller

import (
	"errors"

	"github.com/agentstax/vulkan/pkg/common/logging"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer/controller/datastore"
)

type ProducerController struct {
	Logger logging.Logger

	datastore *datastore.ProducerDatastore
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewProducerController(ds *iDatastore.PostgresDatastore, cfg *ControllerConfig) (*ProducerController, error) {
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

	producerDatastore, err := datastore.NewProducerDatastore(ds, &datastore.ProducerDatastoreConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &ProducerController{
		Logger:    cfg.Logger,
		datastore: producerDatastore,
	}, nil
}
