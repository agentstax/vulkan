package datastore

import (
	"errors"

	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/retry"
)

// MetricsDatastore is the read-only view of the DB-snapshot metrics --
// per (topic, group) queue state, topic-level compaction/group membership,
// and fleet-wide duty health. All DERIVED LIVE from rows that already exist
// (cursor / delivery / lease / maintenance / compaction_head); the DB
// already IS their state store, so every read here works cold, nothing
// needs to be running.
type MetricsDatastore struct {
	Datastore *datastore.PostgresDatastore
	Retry     *retry.DatastoreRetry
	Logger    logger.Logger

	// metricsTopicID caches __system.metrics's own topic id
	metricsTopicID int64
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

	dsRetry, err := retry.NewDatastoreRetry(cfg.Retry, cfg.Logger)
	if err != nil {
		return nil, err
	}

	return &MetricsDatastore{
		Datastore:      ds,
		Retry:          dsRetry,
		Logger:         cfg.Logger,
		metricsTopicID: -1, // invalidates cache
	}, nil
}
