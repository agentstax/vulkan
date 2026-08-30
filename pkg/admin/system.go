package admin

import (
	"context"
	"strings"

	"github.com/agentstax/vulkan/pkg/alert"
	"github.com/agentstax/vulkan/pkg/alert/compactionreadcost"
	alertcontroller "github.com/agentstax/vulkan/pkg/alert/controller"
	"github.com/agentstax/vulkan/pkg/alert/partitioncount"
	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/metrics"
	metricscontroller "github.com/agentstax/vulkan/pkg/metrics/controller"
	"github.com/agentstax/vulkan/pkg/migrate"
	"github.com/agentstax/vulkan/pkg/schedule"
	schedulecontroller "github.com/agentstax/vulkan/pkg/schedule/controller"
	"github.com/agentstax/vulkan/pkg/system"
	systemMigrations "github.com/agentstax/vulkan/pkg/system/migrations"
	"github.com/agentstax/vulkan/pkg/topic"
)

// RegisterSystem stands up the shared control-plane schema every topic uses;
// call it once before registering any topic. Safe to call on every startup:
// cfg is applied on every call, so changing a value and redeploying changes
// the system's topics and its built-in alerts' schedules.
//   - cfg: may be nil or sparse -- WithDefaults fills every field left unset
func (a *MessageAdmin) RegisterSystem(ctx context.Context, cfg *RegisterSystemConfig) error {
	if cfg == nil {
		cfg = &RegisterSystemConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return err
	}

	registered, err := a.systemController.Register(ctx, cfg.System)
	if err != nil {
		return err
	}

	// registerTopic, not RegisterTopic -- the latter guards the __system. prefix
	if _, err := a.registerTopic(ctx, metrics.TopicName, metricscontroller.TopicConfig()); err != nil {
		return err
	}
	if _, err := a.registerTopic(ctx, alert.TopicName, alertcontroller.TopicConfig()); err != nil {
		return err
	}
	if _, err := a.registerTopic(ctx, schedule.TopicName, schedulecontroller.TopicConfig()); err != nil {
		return err
	}

	partitionCountJob, err := partitioncount.NewJob(cfg.PartitionCount)
	if err != nil {
		return err
	}
	compactionReadCostJob, err := compactionreadcost.NewJob(cfg.CompactionReadCost)
	if err != nil {
		return err
	}
	for _, job := range []*alertcontroller.Job{partitionCountJob, compactionReadCostJob} {
		if _, err := a.RegisterSchedule(ctx, job.Name, job.Expression, job.Payload, job.Config); err != nil {
			return err
		}
	}

	// declared after the topics: the alert declarers resolve the job_requests
	// topic to create their consumer groups and worker rows
	owner, err := common.NewSystemOwner(registered.Id)
	if err != nil {
		return err
	}
	for _, declarer := range a.alertDeclarers {
		if err := declarer.Declare(ctx, owner); err != nil {
			return err
		}
	}
	return nil
}

// GetSystem returns the singleton system config. Returns
// migrate.ErrNotRegistered if RegisterSystem hasn't run.
func (a *MessageAdmin) GetSystem(ctx context.Context) (*system.System, error) {
	sys, err := a.systemController.Get(ctx)
	if err != nil {
		return nil, err
	}
	if sys == nil {
		return nil, migrate.ErrNotRegistered
	}
	return sys, nil
}

// MigrateSystem moves the system schema to targetVersion.
// Returns an error ErrNotRegistered if RegisterSystem hasn't run.
func (a *MessageAdmin) MigrateSystem(ctx context.Context, targetVersion int64) error {
	sys, err := a.systemController.Get(ctx)
	if err != nil {
		return err
	}
	if sys == nil {
		return migrate.ErrNotRegistered
	}
	owner, err := common.NewSystemOwner(sys.Id)
	if err != nil {
		return err
	}
	return a.migrateController.RunOnce(ctx, targetVersion, owner, systemMigrations.Registry)
}

// DestroySystem permanently deletes:
// - every registered topic and its messages
// - the system topics
// - schedules
// - consumer groups
// - workers
// - shared control-plane tables
//
// Returns topic.ErrDestroyDisabled unless MessageAdminConfig.AllowDestroy is set.
// Idempotent -- a system already destroyed (or never registered) resolves as
// a no-op, and a re-run after a partial failure resumes where it stopped.
//
// Unless options.Force is set:
//   - a worker instance is still live   -> system.ErrSystemLive
//   - a non-system topic is registered  -> system.ErrTopicsRegistered
func (a *MessageAdmin) DestroySystem(ctx context.Context, options DestroyOptions) error {
	if !a.allowDestroy {
		return topic.ErrDestroyDisabled
	}

	sys, err := a.systemController.Get(ctx)
	if err != nil {
		return err
	}

	// already destroyed -- the end state this call exists to produce holds
	if sys == nil {
		return nil
	}

	if !options.Force {
		if err := a.assertSystemIdle(ctx); err != nil {
			return err
		}
	}

	// each topic through the same delete path DestroyTopic uses, keeping its
	// partition-drain safety against a still-writing producer
	topics, err := a.topicController.List(ctx)
	if err != nil {
		return err
	}
	for _, found := range topics {
		if err := a.topicController.Delete(ctx, found.Id, found.Name); err != nil {
			return err
		}
	}

	return a.systemController.Delete(ctx)
}

// assertSystemIdle is DestroySystem's guard: nothing is running against the
// schema, and no user topic would be taken with it.
func (a *MessageAdmin) assertSystemIdle(ctx context.Context) error {
	// a running manager or consumer heartbeats its worker instances
	workers, err := a.metricsController.WorkerSnapshots(ctx)
	if err != nil {
		return err
	}
	for _, snapshot := range workers {
		if snapshot.LiveInstances > 0 {
			return system.ErrSystemLive.With("worker", snapshot.Name)
		}
	}

	topics, err := a.topicController.List(ctx)
	if err != nil {
		return err
	}
	var names []string
	for _, found := range topics {
		if !isReservedTopicName(found.Name) {
			names = append(names, found.Name)
		}
	}
	if len(names) > 0 {
		return system.ErrTopicsRegistered.With("topics", strings.Join(names, ", "))
	}
	return nil
}
