package admin

import (
	consumermetrics "github.com/agentstax/vulkan/pkg/consumer/metrics"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/migrate"
	"github.com/agentstax/vulkan/pkg/system"
	"github.com/agentstax/vulkan/pkg/topic"
	topicmetrics "github.com/agentstax/vulkan/pkg/topic/metrics"
)

type MessageAdmin struct {
	systemDatastore          *system.SystemDatastore
	topicDatastore           *topic.TopicDatastore
	topicMetricsDatastore    *topicmetrics.TopicMetricsDatastore
	consumerMetricsDatastore *consumermetrics.ConsumerMetricsDatastore
	migrateRunner            *migrate.Runner
	allowDestroy             bool
}

func NewMessageAdmin(ds *datastore.PostgresDatastore, cfg *MessageAdminConfig) (*MessageAdmin, error) {
	if cfg == nil {
		cfg = &MessageAdminConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	systemDatastore, err := system.NewSystemDatastore(ds, cfg.Retry, cfg.Logger)
	if err != nil {
		return nil, err
	}

	topicDatastore, err := topic.NewTopicDatastore(ds, cfg.Retry, cfg.Logger)
	if err != nil {
		return nil, err
	}

	topicMetricsDatastore, err := topicmetrics.NewTopicMetricsDatastore(ds, &topicmetrics.TopicMetricsDatastoreConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	consumerMetricsDatastore, err := consumermetrics.NewConsumerMetricsDatastore(ds, &consumermetrics.ConsumerMetricsDatastoreConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	migrateRunner, err := migrate.NewRunner(ds, cfg.Retry, cfg.Logger)
	if err != nil {
		return nil, err
	}

	return &MessageAdmin{
		systemDatastore:          systemDatastore,
		topicDatastore:           topicDatastore,
		topicMetricsDatastore:    topicMetricsDatastore,
		consumerMetricsDatastore: consumerMetricsDatastore,
		migrateRunner:            migrateRunner,
		allowDestroy:             cfg.AllowDestroy,
	}, nil
}
