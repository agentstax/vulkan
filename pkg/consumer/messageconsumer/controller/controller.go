package controller

import (
	"errors"

	"github.com/agentstax/vulkan/pkg/consumer/messageconsumer/controller/datastore"
	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/logger"
)

type MessageConsumerController struct {
	Logger logger.Logger

	datastore *datastore.MessageConsumerDatastore
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewMessageConsumerController(ds *coredatastore.PostgresDatastore, cfg *ControllerConfig) (*MessageConsumerController, error) {
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

	messageConsumerDatastore, err := datastore.NewMessageConsumerDatastore(ds, &datastore.MessageConsumerDatastoreConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &MessageConsumerController{
		Logger:    cfg.Logger,
		datastore: messageConsumerDatastore,
	}, nil
}
