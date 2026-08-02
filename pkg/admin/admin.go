package admin

import (
	"github.com/agentstax/vulkan/pkg/cron"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/metrics/monitor"
	"github.com/agentstax/vulkan/pkg/migrate"
	"github.com/agentstax/vulkan/pkg/system"
	"github.com/agentstax/vulkan/pkg/topic"
)

type MessageAdmin struct {
	systemDatastore  *system.SystemDatastore
	topicDatastore   *topic.TopicDatastore
	cronJobDatastore *cron.CronJobDatastore
	monitor          *monitor.Monitor
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

	cronJobDatastore, err := cron.NewCronJobDatastore(ds, cfg.Retry, cfg.Logger)
	if err != nil {
		return nil, err
	}

	// no Meter set -- Monitor defaults to noop, exactly what a cold one-shot
	// read (health verdicts, the metrics endpoint) wants: no exporter wiring.
	metricsMonitor, err := monitor.NewMonitor(ds, &monitor.MonitorConfig{
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
		cronJobDatastore: cronJobDatastore,
		monitor:          metricsMonitor,
		migrateRunner:    migrateRunner,
		allowDestroy:     cfg.AllowDestroy,
	}, nil
}
