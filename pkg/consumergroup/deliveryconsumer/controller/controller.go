package controller

import (
	"errors"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/consumergroup/deliveryconsumer/controller/datastore"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
)

type DeliveryConsumerGroupController struct {
	Logger common.Logger

	datastore *datastore.DeliveryConsumerGroupDatastore
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewDeliveryConsumerGroupController(ds *iDatastore.PostgresDatastore, cfg *ControllerConfig) (*DeliveryConsumerGroupController, error) {
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

	deliveryConsumerGroupDatastore, err := datastore.NewDeliveryConsumerGroupDatastore(ds, &datastore.DeliveryConsumerGroupDatastoreConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &DeliveryConsumerGroupController{
		Logger:    cfg.Logger,
		datastore: deliveryConsumerGroupDatastore,
	}, nil
}
