package datastore

import (
	"errors"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/datastore"
)

// CursorAdvancerDatastore owns the committed advance. Every operation is group-scoped,
// idempotent, and concurrent-safe.
type CursorAdvancerDatastore struct {
	Datastore      *datastore.PostgresDatastore
	DatastoreRetry *common.RetryDatastore
	Logger         common.Logger
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewCursorAdvancerDatastore(ds *datastore.PostgresDatastore, cfg *CursorAdvancerDatastoreConfig) (*CursorAdvancerDatastore, error) {
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}
	if cfg == nil {
		cfg = &CursorAdvancerDatastoreConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	datastoreRetry, err := common.NewRetryDatastore(cfg.Retry, cfg.Logger)
	if err != nil {
		return nil, err
	}

	return &CursorAdvancerDatastore{
		Datastore:      ds,
		DatastoreRetry: datastoreRetry,
		Logger:         cfg.Logger,
	}, nil
}
