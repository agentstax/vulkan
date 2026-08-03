package admin

import (
	"github.com/agentstax/vulkan/pkg/cron"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/metrics/monitor"
	"github.com/agentstax/vulkan/pkg/migrate"
	systemcontroller "github.com/agentstax/vulkan/pkg/system/controller"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
	"github.com/agentstax/vulkan/pkg/worker/cronscheduler"
	"github.com/agentstax/vulkan/pkg/worker/janitor"
)

type MessageAdmin struct {
	systemController *systemcontroller.SystemController
	topicController  *topiccontroller.TopicController
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

	cronSchedulerFactory, err := cronscheduler.NewCronSchedulerFactory(ds, &cronscheduler.CronSchedulerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	systemController, err := systemcontroller.NewSystemController(ds, &systemcontroller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	}, cronSchedulerFactory)
	if err != nil {
		return nil, err
	}

	janitorFactory, err := janitor.NewJanitorFactory(ds, &janitor.JanitorConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	topicController, err := topiccontroller.NewTopicController(ds, &topiccontroller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	}, janitorFactory)
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
		systemController: systemController,
		topicController:  topicController,
		cronJobDatastore: cronJobDatastore,
		monitor:          metricsMonitor,
		migrateRunner:    migrateRunner,
		allowDestroy:     cfg.AllowDestroy,
	}, nil
}
