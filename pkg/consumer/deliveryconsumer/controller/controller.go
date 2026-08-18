package controller

import (
	"errors"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/consumer/deliveryconsumer/controller/datastore"
	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
)

type DeliveryConsumerController struct {
	Logger common.Logger

	datastore *datastore.DeliveryConsumerDatastore
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewDeliveryConsumerController(ds *coredatastore.PostgresDatastore, cfg *ControllerConfig) (*DeliveryConsumerController, error) {
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

	deliveryConsumerDatastore, err := datastore.NewDeliveryConsumerDatastore(ds, &datastore.DeliveryConsumerDatastoreConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &DeliveryConsumerController{
		Logger:    cfg.Logger,
		datastore: deliveryConsumerDatastore,
	}, nil
}
