package controller

import (
	"errors"
	"fmt"

	"github.com/agentstax/vulkan/pkg/common/logging"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer/controller/datastore"
	"github.com/agentstax/vulkan/pkg/topic"
)

type ProducerController[Message any] struct {
	Logger logging.Logger

	schemaVersion topic.SchemaVersion
	datastore     *datastore.ProducerDatastore[Message]
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewProducerController[Message any](ds *iDatastore.PostgresDatastore, schemaVersion topic.SchemaVersion, cfg *ControllerConfig) (*ProducerController[Message], error) {
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}
	if schemaVersion < 1 {
		return nil, fmt.Errorf("schemaVersion must be >= 1, got %d", schemaVersion)
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
		Logger:        cfg.Logger,
		schemaVersion: schemaVersion,
		datastore:     producerDatastore,
	}, nil
}
