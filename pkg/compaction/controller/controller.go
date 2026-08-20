package controller

import (
	"errors"

	"github.com/agentstax/vulkan/pkg/common/logging"
	"github.com/agentstax/vulkan/pkg/compaction/controller/datastore"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
)

// CompactionController reads compaction heads -- the write side
// (the head upsert and the FOR UPDATE read inside a produce transaction)
// belongs to the producer.
type CompactionController[Message any] struct {
	Logger logging.Logger

	datastore *datastore.CompactionDatastore
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewCompactionController[Message any](ds *iDatastore.PostgresDatastore, cfg *ControllerConfig) (*CompactionController[Message], error) {
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}
	if cfg == nil {
		cfg = &ControllerConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	compactionDatastore, err := datastore.NewCompactionDatastore(ds, &datastore.CompactionDatastoreConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &CompactionController[Message]{
		Logger:    cfg.Logger,
		datastore: compactionDatastore,
	}, nil
}
