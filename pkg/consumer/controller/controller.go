package controller

import (
	"errors"

	"github.com/agentstax/vulkan/pkg/consumer/controller/datastore"
	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/logger"
)

type ConsumerController struct {
	Logger logger.Logger

	datastore *datastore.ConsumerDatastore
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewConsumerController(ds *coredatastore.PostgresDatastore, cfg *ControllerConfig) (*ConsumerController, error) {
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

	consumerDatastore, err := datastore.NewConsumerDatastore(ds, &datastore.ConsumerDatastoreConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &ConsumerController{
		Logger:    cfg.Logger,
		datastore: consumerDatastore,
	}, nil
}
