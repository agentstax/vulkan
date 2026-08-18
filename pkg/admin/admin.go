package admin

import (
	"github.com/agentstax/vulkan/pkg/alert"
	"github.com/agentstax/vulkan/pkg/alert/compactionreadcost"
	"github.com/agentstax/vulkan/pkg/alert/partitioncount"
	compactioncontroller "github.com/agentstax/vulkan/pkg/compaction/controller"
	consumercontroller "github.com/agentstax/vulkan/pkg/consumer/controller"
	"github.com/agentstax/vulkan/pkg/cron"
	croncontroller "github.com/agentstax/vulkan/pkg/cron/controller"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/metrics"
	metricscontroller "github.com/agentstax/vulkan/pkg/metrics/controller"
	"github.com/agentstax/vulkan/pkg/migrate"
	"github.com/agentstax/vulkan/pkg/producer"
	systemcontroller "github.com/agentstax/vulkan/pkg/system/controller"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
	"github.com/agentstax/vulkan/pkg/worker"
	workercontroller "github.com/agentstax/vulkan/pkg/worker/controller"
	"github.com/agentstax/vulkan/pkg/worker/cronscheduler"
	"github.com/agentstax/vulkan/pkg/worker/janitor"
	"github.com/agentstax/vulkan/pkg/worker/manager"
	"github.com/agentstax/vulkan/pkg/worker/metricscollector"
)

type MessageAdmin struct {
	systemController   *systemcontroller.SystemController
	topicController    *topiccontroller.TopicController
	cronJobController  *croncontroller.CronJobController
	consumerController *consumercontroller.ConsumerController
	jobRequestProducer *producer.Producer[cron.JobRequest]
	alertHeads         *compactioncontroller.CompactionController[alert.Alert]
	// only measurements carry compaction keys on __system.metrics, so this
	// Measurement-typed controller's reads never see an abandoned-routine event
	measurementHeads  *compactioncontroller.CompactionController[metrics.Measurement]
	metricsController *metricscontroller.MetricsController
	workerController  *workercontroller.WorkerController
	migrateController *migrate.Controller
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

	metricsCollectorDefinition, err := metricscollector.NewMetricsCollectorDefinition(ds, &metricscollector.MetricsCollectorConfig{
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
	}, janitorDefinition, cronSchedulerDefinition, metricsCollectorDefinition)
	if err != nil {
		return nil, err
	}

	systemController, err := systemcontroller.NewSystemController(ds, &systemcontroller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	}, cronSchedulerDefinition, metricsCollectorDefinition, managerDefinition)
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

	consumerController, err := consumercontroller.NewConsumerController(ds, &consumercontroller.ControllerConfig{
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
	partitionCountDefinition, err := partitioncount.NewPartitionCountDefinition(ds, &partitioncount.PartitionCountConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}
	compactionReadCostDefinition, err := compactionreadcost.NewCompactionReadCostDefinition(ds, &compactionreadcost.CompactionReadCostConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	migrateController, err := migrate.NewController(ds, &migrate.ControllerConfig{
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
		alertDeclarers:     []worker.Declarer{partitionCountDefinition, compactionReadCostDefinition},
		allowDestroy:       cfg.AllowDestroy,
	}, nil
}
