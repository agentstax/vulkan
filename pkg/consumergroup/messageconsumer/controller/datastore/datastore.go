package datastore

import (
	"errors"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/datastore"
)

type MessageConsumerGroupDatastore struct {
	Datastore      *datastore.PostgresDatastore
	DatastoreRetry *common.RetryDatastore // default Wrap classification covers everything except Commit/PartialCommit -- classified inline at that call site
	Logger         common.Logger
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewMessageConsumerGroupDatastore(ds *datastore.PostgresDatastore, cfg *MessageConsumerGroupDatastoreConfig) (*MessageConsumerGroupDatastore, error) {
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}
	if cfg == nil {
		cfg = &MessageConsumerGroupDatastoreConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	datastoreRetry, err := common.NewRetryDatastore(cfg.Retry, cfg.Logger)
	if err != nil {
		return nil, err
	}

	return &MessageConsumerGroupDatastore{
		Datastore:      ds,
		DatastoreRetry: datastoreRetry,
		Logger:         cfg.Logger,
	}, nil
}
