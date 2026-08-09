package admin

import (
	"github.com/agentstax/vulkan/pkg/cron"
	"github.com/agentstax/vulkan/pkg/datastore"
	metricscontroller "github.com/agentstax/vulkan/pkg/metrics/controller"
	"github.com/agentstax/vulkan/pkg/migrate"
	systemcontroller "github.com/agentstax/vulkan/pkg/system/controller"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
	"github.com/agentstax/vulkan/pkg/worker/cronscheduler"
	"github.com/agentstax/vulkan/pkg/worker/janitor"
	"github.com/agentstax/vulkan/pkg/worker/manager"
)

type MessageAdmin struct {
	systemController  *systemcontroller.SystemController
	topicController   *topiccontroller.TopicController
	cronJobDatastore  *cron.CronJobDatastore
	metricsController *metricscontroller.MetricsController
	migrateRunner     *migrate.Runner
	allowDestroy      bool
}

func NewMessageAdmin(ds *datastore.PostgresDatastore, cfg *MessageAdminConfig) (*MessageAdmin, error) {
	if cfg == nil {
		cfg = &MessageAdminConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	cronSchedulerDefinition, err := cronscheduler.NewCronSchedulerDefinition(ds, &cronscheduler.CronSchedulerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	janitorDefinition, err := janitor.NewJanitorDefinition(ds, &janitor.JanitorConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	// a declarer here, never run -- admin creates manager rows, it doesn't claim them
	managerDefinition, err := manager.NewManagerDefinition(ds, &manager.ManagerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	}, janitorDefinition, cronSchedulerDefinition)
	if err != nil {
		return nil, err
	}

	systemController, err := systemcontroller.NewSystemController(ds, &systemcontroller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	}, cronSchedulerDefinition, managerDefinition)
	if err != nil {
		return nil, err
	}

	topicController, err := topiccontroller.NewTopicController(ds, &topiccontroller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	}, janitorDefinition, managerDefinition)
	if err != nil {
		return nil, err
	}

	cronJobDatastore, err := cron.NewCronJobDatastore(ds, cfg.Retry, cfg.Logger)
	if err != nil {
		return nil, err
	}

	metricsController, err := metricscontroller.NewMetricsController(ds, &metricscontroller.ControllerConfig{
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
		systemController:  systemController,
		topicController:   topicController,
		cronJobDatastore:  cronJobDatastore,
		metricsController: metricsController,
		migrateRunner:     migrateRunner,
		allowDestroy:      cfg.AllowDestroy,
	}, nil
}
