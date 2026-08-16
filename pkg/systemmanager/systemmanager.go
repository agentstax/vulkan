package systemmanager

import (
	"context"
	"errors"

	"github.com/agentstax/vulkan/pkg/alert/compactionreadcost"
	"github.com/agentstax/vulkan/pkg/alert/partitioncount"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/migrate"
	"github.com/agentstax/vulkan/pkg/worker"
	"github.com/agentstax/vulkan/pkg/worker/cronscheduler"
	"github.com/agentstax/vulkan/pkg/worker/janitor"
	"github.com/agentstax/vulkan/pkg/worker/manager"
	"github.com/agentstax/vulkan/pkg/worker/metricscollector"
	"github.com/agentstax/vulkan/pkg/worker/waterline"
)

// SystemManager keeps the deployment's upkeep running with no user process
// up: it claims the system's manager row and reconciles every worker row in
// the deployment, the alerts' consumers included. Safe to run N-way --
// worker claims arbitrate who runs what.
type SystemManager struct {
	Config *SystemManagerConfig
	Logger logger.Logger

	ds      *datastore.PostgresDatastore
	manager *manager.ManagerDefinition
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

	janitorDefinition, err := janitor.NewJanitorDefinition(ds, &janitor.JanitorConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}
	cronSchedulerDefinition, err := cronscheduler.NewCronSchedulerDefinition(ds, &cronscheduler.CronSchedulerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}
	// waterlines keep rolling -- and retention keeps moving -- for groups
	// whose consumers are offline
	waterlineDefinition, err := waterline.NewWaterlineDefinition(ds, &waterline.WaterlineConfig{
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

	provisioners := []worker.Provisioner{janitorDefinition, cronSchedulerDefinition, metricsCollectorDefinition, waterlineDefinition, partitionCountDefinition, compactionReadCostDefinition}
	managerDefinition, err := manager.NewManagerDefinition(ds, &manager.ManagerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	}, provisioners...)
	if err != nil {
		return nil, err
	}

	return &SystemManager{
		Config:  cfg,
		Logger:  cfg.Logger,
		ds:      ds,
		manager: managerDefinition,
	}, nil
}

// Run claims and reconciles until ctx cancels; a requested stop returns nil.
// Returns migrate.ErrNotRegistered when no system has been registered.
func (s *SystemManager) Run(ctx context.Context) error {
	owner, err := migrate.SystemOwner(ctx, s.ds.Pool)
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
