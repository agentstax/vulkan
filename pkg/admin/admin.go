package admin

import (
	"github.com/agentstax/vulkan/pkg/alert/compactionreadcost"
	"github.com/agentstax/vulkan/pkg/alert/partitioncount"
	"github.com/agentstax/vulkan/pkg/cron"
	croncontroller "github.com/agentstax/vulkan/pkg/cron/controller"
	"github.com/agentstax/vulkan/pkg/datastore"
	metricscontroller "github.com/agentstax/vulkan/pkg/metrics/controller"
	"github.com/agentstax/vulkan/pkg/migrate"
	"github.com/agentstax/vulkan/pkg/producer"
	systemcontroller "github.com/agentstax/vulkan/pkg/system/controller"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
	"github.com/agentstax/vulkan/pkg/worker"
	"github.com/agentstax/vulkan/pkg/worker/cronscheduler"
	"github.com/agentstax/vulkan/pkg/worker/janitor"
	"github.com/agentstax/vulkan/pkg/worker/manager"
)

type MessageAdmin struct {
	systemController   *systemcontroller.SystemController
	topicController    *topiccontroller.TopicController
	cronJobController  *croncontroller.CronJobController
	jobRequestProducer *producer.Producer[cron.JobRequest]
	metricsController  *metricscontroller.MetricsController
	migrateRunner      *migrate.Runner
	alertDeclarers     []worker.Declarer
	allowDestroy       bool
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
	}, janitorDefinition)
	if err != nil {
		return nil, err
	}

	cronJobController, err := croncontroller.NewCronJobController(ds, &croncontroller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	jobRequestProducer, err := producer.NewProducer[cron.JobRequest](ds, &producer.ProducerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
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

	// declarers here, never run -- RegisterSystem creates the alerts' consumer
	// groups and worker rows, the system manager claims them
	partitionCountDefinition, err := partitioncount.NewPartitionCountDefinition(ds, &partitioncount.DefinitionConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}
	compactionReadCostDefinition, err := compactionreadcost.NewCompactionReadCostDefinition(ds, &compactionreadcost.DefinitionConfig{
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
		systemController:   systemController,
		topicController:    topicController,
		cronJobController:  cronJobController,
		jobRequestProducer: jobRequestProducer,
		metricsController:  metricsController,
		migrateRunner:      migrateRunner,
		alertDeclarers:     []worker.Declarer{partitionCountDefinition, compactionReadCostDefinition},
		allowDestroy:       cfg.AllowDestroy,
	}, nil
}
