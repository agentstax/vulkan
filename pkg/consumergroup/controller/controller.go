package controller

import (
	"errors"

	"github.com/agentstax/vulkan/pkg/common/logging"
	"github.com/agentstax/vulkan/pkg/consumergroup/controller/datastore"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
)

type ConsumerGroupController struct {
	Logger logging.Logger

	datastore *datastore.ConsumerGroupDatastore
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewConsumerGroupController(ds *iDatastore.PostgresDatastore, cfg *ControllerConfig) (*ConsumerGroupController, error) {
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

	consumerDatastore, err := datastore.NewConsumerGroupDatastore(ds, &datastore.ConsumerGroupDatastoreConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &ConsumerGroupController{
		Logger:    cfg.Logger,
		datastore: consumerDatastore,
	}, nil
}
