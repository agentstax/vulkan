package admin

import (
	"github.com/agentstax/vulkan/pkg/datastore"
	metricsDatastore "github.com/agentstax/vulkan/pkg/metrics/datastore"
	"github.com/agentstax/vulkan/pkg/migrate"
	"github.com/agentstax/vulkan/pkg/system"
	"github.com/agentstax/vulkan/pkg/topic"
)

type MessageAdmin struct {
	systemDatastore  *system.SystemDatastore
	topicDatastore   *topic.TopicDatastore
	metricsDatastore *metricsDatastore.MetricsDatastore
	migrateRunner    *migrate.Runner
	allowDestroy     bool
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

	metricsDatastore, err := metricsDatastore.NewMetricsDatastore(ds, &metricsDatastore.MetricsDatastoreConfig{
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
		systemDatastore:  systemDatastore,
		topicDatastore:   topicDatastore,
		metricsDatastore: metricsDatastore,
		migrateRunner:    migrateRunner,
		allowDestroy:     cfg.AllowDestroy,
	}, nil
}
