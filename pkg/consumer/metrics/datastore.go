package metrics

import (
	"os"

	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/retry"
)

type consumerMetricsDatastore struct {
	Datastore *datastore.PostgresDatastore
	Retry     *retry.DatastoreRetry // default Wrap classification covers everything except Commit/PartialCommit -- classified inline at that call site
	Logger    logger.Logger
}

type ConsumerMetricsDatastoreConfig struct {
	Logger logger.Logger // pass your own *slog.Logger (own Handler) or anything satisfying logger.Logger. Default: text logger to stdout, warn level and up.
	Retry  *retry.Policy // Default: retry.NewDefaultRetryPolicy(). Metric polling may want a shorter policy than the default.
}

func (c *ConsumerMetricsDatastoreConfig) withDefaults() *ConsumerMetricsDatastoreConfig {
	if c.Logger == nil {
		c.Logger = logger.NewDefaultLogger(os.Stdout)
	}
	c.Retry = c.Retry.WithDefaults()
	return c
}

func NewConsumerDatastore(ds *datastore.PostgresDatastore, cfg *ConsumerMetricsDatastoreConfig) *consumerMetricsDatastore {
	if cfg == nil {
		cfg = &ConsumerMetricsDatastoreConfig{}
	}
	cfg.withDefaults()
	return &consumerMetricsDatastore{
		Datastore: ds,
		Retry:     retry.NewDatastoreRetry(cfg.Retry.MaxRetries, cfg.Retry.BaseDelay, cfg.Retry.MaxDelay, cfg.Retry.Exponent, cfg.Logger),
		Logger:    cfg.Logger,
	}
}
