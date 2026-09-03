package systemmanager

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"

	"github.com/agentstax/vulkan/pkg/alert/compactionreadcost"
	"github.com/agentstax/vulkan/pkg/alert/partitioncount"
	"github.com/agentstax/vulkan/pkg/alert/workerliveness"
	"github.com/agentstax/vulkan/pkg/common/logging"
	"github.com/agentstax/vulkan/pkg/consume/cursoradvancer"
	consumejanitor "github.com/agentstax/vulkan/pkg/consume/janitor"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/metrics/collector"
	migratecontroller "github.com/agentstax/vulkan/pkg/migrate/controller"
	scheduleproducer "github.com/agentstax/vulkan/pkg/schedule/producer"
	"github.com/agentstax/vulkan/pkg/system"
	topicjanitor "github.com/agentstax/vulkan/pkg/topic/janitor"
	"github.com/agentstax/vulkan/pkg/worker"
	"github.com/agentstax/vulkan/pkg/worker/manager"
)

// SystemManager keeps the deployment's upkeep running with no user process
// up: it claims the system's manager row and reconciles every worker row in
// the deployment, the alerts' consumers included. Safe to run N-way -- the
// manager row's own claim admits one reconcile loop at a time, and the
// spawned workers' claims arbitrate the rest.
type SystemManager struct {
	Config *SystemManagerConfig
	Logger logging.Logger

	ds                *datastore.PostgresDatastore
	manager           *manager.ManagerProvisioner
	migrateController *migratecontroller.Controller
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewSystemManager(ds *datastore.PostgresDatastore, cfg *SystemManagerConfig) (*SystemManager, error) {
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}
	if cfg == nil {
		cfg = &SystemManagerConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	cfg.Logger = logging.NewPipelineLogger(cfg.Logger, &logging.PipelineLoggerConfig{Buffer: true, Suppress: true})

	topicJanitorProvisioner, err := topicjanitor.NewJanitorProvisioner(ds, &topicjanitor.JanitorConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	consumerGroupJanitorProvisioner, err := consumejanitor.NewJanitorProvisioner(ds, &consumejanitor.JanitorConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	scheduleProducerProvisioner, err := scheduleproducer.NewScheduleProducerProvisioner(ds, &scheduleproducer.ScheduleProducerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	// committed keeps advancing -- and retention keeps moving -- for groups
	// whose consumers are offline
	cursorAdvancerProvisioner, err := cursoradvancer.NewCursorAdvancerProvisioner(ds, &cursoradvancer.CursorAdvancerConfig{
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

	workerLivenessProvisioner, err := workerliveness.NewWorkerLivenessProvisioner(ds, &workerliveness.WorkerLivenessConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	provisioners := []worker.Provisioner{topicJanitorProvisioner, consumerGroupJanitorProvisioner, scheduleProducerProvisioner, metricsCollectorProvisioner, cursorAdvancerProvisioner, partitionCountProvisioner, compactionReadCostProvisioner, workerLivenessProvisioner}
	managerProvisioner, err := manager.NewManagerProvisioner(ds, 1, &manager.ManagerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	}, provisioners...)
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

	return &SystemManager{
		Config:            cfg,
		Logger:            cfg.Logger,
		ds:                ds,
		manager:           managerProvisioner,
		migrateController: migrateController,
	}, nil
}

// Run keeps the deployment's upkeep running until ctx cancels, and returns
// nil then. Safe to call N times, in one process or many -- the manager
// row's claim admits one reconcile loop at a time and every other call
// retries the claim. A life that ends on its own is logged and claimed
// again behind Config.RunRetry; errors before the first claim return to
// the caller.
// Returns migrate.ErrNotRegistered when no system has been registered.
func (s *SystemManager) Run(ctx context.Context) error {
	owner, err := s.migrateController.SystemOwner(ctx)
	if err != nil {
		return err
	}
	runner, err := manager.NewRunner(s.manager, owner, &manager.RunnerConfig{
		Logger: s.Logger,
	})
	if err != nil {
		return err
	}

	for attempt := 0; ; attempt++ {
		err := runner.Run(ctx)
		if ctx.Err() != nil {
			return nil
		}

		// re-jittered every retry -- replicas that hit the same fault must
		// not retry in step
		jitter := 1 + s.Config.JitterFraction*(2*rand.Float64()-1)
		delay := time.Duration(float64(s.Config.RunRetry.CalculateDelay(min(attempt, s.Config.RunRetry.MaxRetries))) * jitter)
		if err != nil {
			s.Logger.ErrorContext(ctx, system.EventSystemManagerStopped.Message,
				"code", system.EventSystemManagerStopped.Code,
				"attempt", attempt+1,
				"delay", delay,
				"error", err)
		}

		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}
