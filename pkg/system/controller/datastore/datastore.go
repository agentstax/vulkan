package datastore

import (
	"errors"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/common/logging"
	"github.com/agentstax/vulkan/pkg/datastore"
)

// SystemDatastore owns the shared control-plane tables.
// Tables:
// - system_config
// - topic_config
// - topic_config_log
// - consumer_group_config
// - worker_config
// - worker_config_log
// - worker_instance
// - schedule_config
// - schedule_cursor
// - migration_log
type SystemDatastore struct {
	Datastore      *datastore.PostgresDatastore
	DatastoreRetry *common.RetryDatastore
	Logger         logging.Logger
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewSystemDatastore(ds *datastore.PostgresDatastore, cfg *SystemDatastoreConfig) (*SystemDatastore, error) {
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}
	if cfg == nil {
		cfg = &SystemDatastoreConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	datastoreRetry, err := common.NewRetryDatastore(cfg.Retry, cfg.Logger)
	if err != nil {
		return nil, err
	}

	return &SystemDatastore{
		Datastore:      ds,
		DatastoreRetry: datastoreRetry,
		Logger:         cfg.Logger,
	}, nil
}
