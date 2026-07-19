package metrics

import (
	"errors"

	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/retry"
)

type consumerMetricsDatastore struct {
	Datastore *datastore.PostgresDatastore
	Retry     *retry.DatastoreRetry // default Wrap classification covers everything except Commit/PartialCommit -- classified inline at that call site
	Logger    logger.Logger
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewConsumerDatastore(ds *datastore.PostgresDatastore, cfg *ConsumerMetricsDatastoreConfig) (*consumerMetricsDatastore, error) {
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}
	if cfg == nil {
		cfg = &ConsumerMetricsDatastoreConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	dsRetry, err := retry.NewDatastoreRetry(cfg.Retry, cfg.Logger)
	if err != nil {
		return nil, err
	}

	return &consumerMetricsDatastore{
		Datastore: ds,
		Retry:     dsRetry,
		Logger:    cfg.Logger,
	}, nil
}
