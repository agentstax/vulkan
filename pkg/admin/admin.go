package admin

import (
	"github.com/agentstax/vulkan/pkg/alert"
	"github.com/agentstax/vulkan/pkg/alert/compactionreadcost"
	"github.com/agentstax/vulkan/pkg/alert/partitioncount"
	compactioncontroller "github.com/agentstax/vulkan/pkg/compaction/controller"
	consumergroupcontroller "github.com/agentstax/vulkan/pkg/consumergroup/controller"
	"github.com/agentstax/vulkan/pkg/cron"
	croncontroller "github.com/agentstax/vulkan/pkg/cron/controller"
	"github.com/agentstax/vulkan/pkg/cron/scheduler"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/metrics"
	"github.com/agentstax/vulkan/pkg/metrics/collector"
	metricscontroller "github.com/agentstax/vulkan/pkg/metrics/controller"
	migratecontroller "github.com/agentstax/vulkan/pkg/migrate/controller"
	"github.com/agentstax/vulkan/pkg/producer"
	systemcontroller "github.com/agentstax/vulkan/pkg/system/controller"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
	"github.com/agentstax/vulkan/pkg/topic/janitor"
	"github.com/agentstax/vulkan/pkg/worker"
	workercontroller "github.com/agentstax/vulkan/pkg/worker/controller"
	"github.com/agentstax/vulkan/pkg/worker/manager"
)

type MessageAdmin struct {
	systemController   *systemcontroller.SystemController
	topicController    *topiccontroller.TopicController
	cronJobController  *croncontroller.CronJobController
	consumerController *consumergroupcontroller.ConsumerGroupController
	jobRequestProducer *producer.Producer[cron.JobRequest]
	alertHeads         *compactioncontroller.CompactionController[alert.Alert]
	// only measurements carry compaction keys on __system.metrics, so this
	// Measurement-typed controller's reads never see an abandoned-routine event
	measurementHeads  *compactioncontroller.CompactionController[metrics.Measurement]
	metricsController *metricscontroller.MetricsController
	workerController  *workercontroller.WorkerController
	migrateController *migratecontroller.Controller
	alertDeclarers    []worker.Declarer
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

	cronSchedulerProvisioner, err := scheduler.NewCronSchedulerProvisioner(ds, &scheduler.CronSchedulerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	janitorProvisioner, err := janitor.NewJanitorProvisioner(ds, &janitor.JanitorConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	metricsCollectorProvisioner, err := collector.NewMetricsCollectorProvisioner(ds, &collector.MetricsCollectorConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	// a declarer here, never run -- admin creates manager rows, it doesn't claim them
	managerProvisioner, err := manager.NewManagerProvisioner(ds, &manager.ManagerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	}, janitorProvisioner, cronSchedulerProvisioner, metricsCollectorProvisioner)
	if err != nil {
		return nil, err
	}

	systemController, err := systemcontroller.NewSystemController(ds, &systemcontroller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	}, cronSchedulerProvisioner, metricsCollectorProvisioner, managerProvisioner)
	if err != nil {
		return nil, err
	}

	topicController, err := topiccontroller.NewTopicController(ds, &topiccontroller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	}, janitorProvisioner)
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

	alertHeads, err := compactioncontroller.NewCompactionController[alert.Alert](ds, &compactioncontroller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	measurementHeads, err := compactioncontroller.NewCompactionController[metrics.Measurement](ds, &compactioncontroller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	consumerController, err := consumergroupcontroller.NewConsumerGroupController(ds, &consumergroupcontroller.ControllerConfig{
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

	workerController, err := workercontroller.NewWorkerController(ds, &workercontroller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	// declarers here, never run -- RegisterSystem creates the alerts' consumer
	// groups and worker rows, the system manager claims them
	partitionCountProvisioner, err := partitioncount.NewPartitionCountProvisioner(ds, &partitioncount.PartitionCountConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}
	compactionReadCostProvisioner, err := compactionreadcost.NewCompactionReadCostProvisioner(ds, &compactionreadcost.CompactionReadCostConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	migrateController, err := migratecontroller.NewController(ds, &migratecontroller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &MessageAdmin{
		systemController:   systemController,
		topicController:    topicController,
		cronJobController:  cronJobController,
		consumerController: consumerController,
		jobRequestProducer: jobRequestProducer,
		alertHeads:         alertHeads,
		measurementHeads:   measurementHeads,
		metricsController:  metricsController,
		workerController:   workerController,
		migrateController:  migrateController,
		alertDeclarers:     []worker.Declarer{partitionCountProvisioner, compactionReadCostProvisioner},
		allowDestroy:       cfg.AllowDestroy,
	}, nil
}
