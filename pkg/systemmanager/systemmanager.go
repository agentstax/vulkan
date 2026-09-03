package systemmanager

import (
	"context"
	"errors"

	"github.com/agentstax/vulkan/pkg/alert/compactionreadcost"
	"github.com/agentstax/vulkan/pkg/alert/partitioncount"
	"github.com/agentstax/vulkan/pkg/alert/workerliveness"
	"github.com/agentstax/vulkan/pkg/common/logging"
	"github.com/agentstax/vulkan/pkg/concurrency"
	"github.com/agentstax/vulkan/pkg/consumergroup/cursoradvancer"
	consumergroupjanitor "github.com/agentstax/vulkan/pkg/consumergroup/janitor"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/metrics/collector"
	migratecontroller "github.com/agentstax/vulkan/pkg/migrate/controller"
	scheduleproducer "github.com/agentstax/vulkan/pkg/schedule/producer"
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
	permit            *concurrency.Permit // held for the length of a Run call
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

	consumerGroupJanitorProvisioner, err := consumergroupjanitor.NewJanitorProvisioner(ds, &consumergroupjanitor.JanitorConfig{
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

	permit, err := concurrency.NewPermit()
	if err != nil {
		return nil, err
	}

	return &SystemManager{
		Config:            cfg,
		Logger:            cfg.Logger,
		ds:                ds,
		manager:           managerProvisioner,
		migrateController: migrateController,
		permit:            permit,
	}, nil
}

// Run claims and reconciles until ctx cancels; a requested stop returns nil.
// One Run at a time per instance -- a second concurrent call is refused.
// Returns migrate.ErrNotRegistered when no system has been registered.
func (s *SystemManager) Run(ctx context.Context) error {
	// the row's claim gate caps live instances deployment-wide; this refuses a
	// second Run in-process rather than leaving it in a claim-retry loop
	release, ok := s.permit.Acquire()
	if !ok {
		return errors.New("system manager already running")
	}
	defer release()

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
	return runner.Run(ctx)
}
