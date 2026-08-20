package datastore

import (
	"errors"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/datastore"
)

type ExceptionConsumerGroupDatastore struct {
	Datastore      *datastore.PostgresDatastore
	DatastoreRetry *common.RetryDatastore
	Logger         common.Logger
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewExceptionConsumerGroupDatastore(ds *datastore.PostgresDatastore, cfg *ExceptionConsumerGroupDatastoreConfig) (*ExceptionConsumerGroupDatastore, error) {
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}
	if cfg == nil {
		cfg = &ExceptionConsumerGroupDatastoreConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	datastoreRetry, err := common.NewRetryDatastore(cfg.Retry, cfg.Logger)
	if err != nil {
		return nil, err
	}

	return &ExceptionConsumerGroupDatastore{
		Datastore:      ds,
		DatastoreRetry: datastoreRetry,
		Logger:         cfg.Logger,
	}, nil
}
