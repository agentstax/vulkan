package datastore

import (
	"errors"

	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/retry"
)

type WorkerDatastore struct {
	Datastore      *datastore.PostgresDatastore
	DatastoreRetry *retry.DatastoreRetry
	Logger         logger.Logger
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewWorkerDatastore(ds *datastore.PostgresDatastore, cfg *WorkerDatastoreConfig) (*WorkerDatastore, error) {
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}
	if cfg == nil {
		cfg = &WorkerDatastoreConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	datastoreRetry, err := retry.NewDatastoreRetry(cfg.Retry, cfg.Logger)
	if err != nil {
		return nil, err
	}

	return &WorkerDatastore{
		Datastore:      ds,
		DatastoreRetry: datastoreRetry,
		Logger:         cfg.Logger,
	}, nil
}
