package controller

import (
	"errors"

	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/producer/controller/datastore"
)

type ProducerController[Message any] struct {
	Logger logger.Logger

	datastore *datastore.ProducerDatastore[Message]
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewProducerController[Message any](ds *coredatastore.PostgresDatastore, cfg *ControllerConfig) (*ProducerController[Message], error) {
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

	producerDatastore, err := datastore.NewProducerDatastore[Message](ds, &datastore.ProducerDatastoreConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &ProducerController[Message]{
		Logger:    cfg.Logger,
		datastore: producerDatastore,
	}, nil
}
