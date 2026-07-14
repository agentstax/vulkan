package metrics

import (
	"os"
	"time"

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
}

func (c *ConsumerMetricsDatastoreConfig) withDefaults() *ConsumerMetricsDatastoreConfig {
	if c.Logger == nil {
		c.Logger = logger.NewDefaultLogger(os.Stdout)
	}
	return c
}

func NewConsumerDatastore(ds *datastore.PostgresDatastore, cfg *ConsumerMetricsDatastoreConfig) *consumerMetricsDatastore {
	if cfg == nil {
		cfg = &ConsumerMetricsDatastoreConfig{}
	}
	cfg.withDefaults()
	return &consumerMetricsDatastore{
		Datastore: ds,
		// TODO - need to think through if it makes sense for metric polling to have such long timeouts or not
		Retry:  retry.NewDatastoreRetry(6, time.Second, 5*time.Minute, 2, cfg.Logger), // TODO - make this user config driven eventually
		Logger: cfg.Logger,
	}
}
