package datastore

import (
	"errors"

	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/retry"
)

type ProducerDatastore[Message any] struct {
	Datastore      *datastore.PostgresDatastore
	DatastoreRetry *retry.DatastoreRetry // default Wrap classification covers everything except Commit -- classified inline at that call site
	Logger         logger.Logger
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewProducerDatastore[Message any](ds *datastore.PostgresDatastore, cfg *ProducerDatastoreConfig) (*ProducerDatastore[Message], error) {
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}
	if cfg == nil {
		cfg = &ProducerDatastoreConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	datastoreRetry, err := retry.NewDatastoreRetry(cfg.Retry, cfg.Logger)
	if err != nil {
		return nil, err
	}

	return &ProducerDatastore[Message]{
		Datastore:      ds,
		DatastoreRetry: datastoreRetry,
		Logger:         cfg.Logger,
	}, nil
}
