package controller

import (
	"errors"

	"github.com/agentstax/vulkan/pkg/common/logging"
	"github.com/agentstax/vulkan/pkg/consumergroup/exceptionconsumer/controller/datastore"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
)

type ExceptionConsumerGroupController struct {
	Logger logging.Logger

	datastore *datastore.ExceptionConsumerGroupDatastore
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewExceptionConsumerGroupController(ds *iDatastore.PostgresDatastore, cfg *ControllerConfig) (*ExceptionConsumerGroupController, error) {
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

	exceptionConsumerGroupDatastore, err := datastore.NewExceptionConsumerGroupDatastore(ds, &datastore.ExceptionConsumerGroupDatastoreConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &ExceptionConsumerGroupController{
		Logger:    cfg.Logger,
		datastore: exceptionConsumerGroupDatastore,
	}, nil
}
