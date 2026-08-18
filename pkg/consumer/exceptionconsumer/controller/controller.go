package controller

import (
	"errors"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/consumer/exceptionconsumer/controller/datastore"
	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
)

type ExceptionConsumerController struct {
	Logger common.Logger

	datastore *datastore.ExceptionConsumerDatastore
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewExceptionConsumerController(ds *coredatastore.PostgresDatastore, cfg *ControllerConfig) (*ExceptionConsumerController, error) {
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

	exceptionConsumerDatastore, err := datastore.NewExceptionConsumerDatastore(ds, &datastore.ExceptionConsumerDatastoreConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &ExceptionConsumerController{
		Logger:    cfg.Logger,
		datastore: exceptionConsumerDatastore,
	}, nil
}
