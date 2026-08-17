package datastore

import (
	"errors"

	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/retry"
)

// MetricsDatastore derives every snapshot live from rows other domains
// already maintain.
type MetricsDatastore struct {
	Datastore      *datastore.PostgresDatastore
	DatastoreRetry *retry.DatastoreRetry
	Logger         logger.Logger
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewMetricsDatastore(ds *datastore.PostgresDatastore, cfg *MetricsDatastoreConfig) (*MetricsDatastore, error) {
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}
	if cfg == nil {
		cfg = &MetricsDatastoreConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	datastoreRetry, err := retry.NewDatastoreRetry(cfg.Retry, cfg.Logger)
	if err != nil {
		return nil, err
	}

	return &MetricsDatastore{
		Datastore:      ds,
		DatastoreRetry: datastoreRetry,
		Logger:         cfg.Logger,
	}, nil
}
