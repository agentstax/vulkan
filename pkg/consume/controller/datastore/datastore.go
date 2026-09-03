package datastore

import (
	"errors"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/common/logging"
	"github.com/agentstax/vulkan/pkg/datastore"
)

type ConsumeDatastore struct {
	Datastore      *datastore.PostgresDatastore
	DatastoreRetry *common.RetryDatastore
	Logger         logging.Logger
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewConsumeDatastore(ds *datastore.PostgresDatastore, cfg *ConsumeDatastoreConfig) (*ConsumeDatastore, error) {
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}
	if cfg == nil {
		cfg = &ConsumeDatastoreConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	datastoreRetry, err := common.NewRetryDatastore(cfg.Retry, cfg.Logger)
	if err != nil {
		return nil, err
	}

	return &ConsumeDatastore{
		Datastore:      ds,
		DatastoreRetry: datastoreRetry,
		Logger:         cfg.Logger,
	}, nil
}
