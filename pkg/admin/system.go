package admin

import (
	"context"
	"fmt"
	"strings"

	"github.com/agentstax/vulkan/pkg/alert"
	"github.com/agentstax/vulkan/pkg/alert/compactionreadcost"
	"github.com/agentstax/vulkan/pkg/alert/partitioncount"
	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/cron"
	"github.com/agentstax/vulkan/pkg/metrics"
	"github.com/agentstax/vulkan/pkg/migrate"
	"github.com/agentstax/vulkan/pkg/system"
	systemcontroller "github.com/agentstax/vulkan/pkg/system/controller"
	systemMigrations "github.com/agentstax/vulkan/pkg/system/migrations"
	"github.com/agentstax/vulkan/pkg/topic"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
)

// RegisterSystem stands up the shared control-plane schema every topic uses;
// call it once before registering any topic. Idempotent.
//   - cfg: may be nil or sparse -- WithDefaults fills every field left unset
func (a *MessageAdmin) RegisterSystem(ctx context.Context, cfg *systemcontroller.SystemConfig) error {
	registered, err := a.systemController.RegisterSystem(ctx, cfg)
	if err != nil {
		return err
	}

	// Make sure the system's owned topics are registered:
	// - __system.metrics
	// - __system.alerts
	// - __system.job_requests
	if err := a.ensureSystemTopic(ctx, metrics.TopicName, metrics.TopicConfig()); err != nil {
		return err
	}
	if err := a.ensureSystemTopic(ctx, alert.TopicName, alert.TopicConfig()); err != nil {
		return err
	}
	if err := a.ensureSystemTopic(ctx, cron.TopicName, cron.TopicConfig()); err != nil {
		return err
	}

	partitionCountJob, err := partitioncount.NewJob()
	if err != nil {
		return err
	}
	compactionReadCostJob, err := compactionreadcost.NewJob()
	if err != nil {
		return err
	}
	for _, job := range []*alert.Job{partitionCountJob, compactionReadCostJob} {
		if err := a.ensureSystemCronJob(ctx, job); err != nil {
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

// ensureSystemCronJob creates the job only when missing. RegisterCronJob is
// not used directly -- its config-mismatch check would error on a job an
// operator has altered after creation.
func (a *MessageAdmin) ensureSystemCronJob(ctx context.Context, job *alert.Job) error {
	existing, err := a.cronJobController.GetCronJob(ctx, job.Name)
	if err != nil {
		return err
	}
	if existing != nil {
		return nil
	}

	_, err = a.RegisterCronJob(ctx, job.Name, job.Schedule, job.Data, job.Config)
	return err
}

func (a *MessageAdmin) ensureSystemTopic(ctx context.Context, name string, cfg *topiccontroller.TopicConfig) error {
	existing, err := a.topicController.GetTopic(ctx, name, topic.SchemaVersion(1))
	if err != nil {
		return err
	}
	if existing != nil {
		return nil
	}

	// registerTopic bypasses the __system. reserved-name guard that RegisterTopic enforces.
	_, err = a.registerTopic(ctx, name, topic.SchemaVersion(1), cfg)
	return err
}

// GetSystem returns the singleton system config. Returns
// migrate.ErrNotRegistered if RegisterSystem hasn't run.
func (a *MessageAdmin) GetSystem(ctx context.Context) (*system.System, error) {
	sys, err := a.systemController.GetSystem(ctx)
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
	sys, err := a.systemController.GetSystem(ctx)
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
	return a.migrateRunner.RunOnce(ctx, targetVersion, owner, systemMigrations.Registry)
}

// DestroySystem permanently deletes:
// - every registered topic and its messages
// - the system topics
// - cron jobs
// - consumer groups
// - workers
// - shared control-plane tables
//
// Returns ErrDestroyDisabled unless MessageAdminConfig.AllowDestroy is set.
// Idempotent -- a system already destroyed (or never registered) resolves as
// a no-op, and a re-run after a partial failure resumes where it stopped.
//
// Unless opts.Force is set:
//   - a worker instance is still live   -> system.ErrSystemLive
//   - a non-system topic is registered  -> system.ErrTopicsRegistered
func (a *MessageAdmin) DestroySystem(ctx context.Context, opts DestroyOptions) error {
	if !a.allowDestroy {
		return ErrDestroyDisabled
	}

	sys, err := a.systemController.GetSystem(ctx)
	if err != nil {
		return err
	}
	// already destroyed -- the end state this call exists to produce holds
	if sys == nil {
		return nil
	}

	if !opts.Force {
		if err := a.assertSystemIdle(ctx); err != nil {
			return err
		}
	}

	// each topic through the same delete path DestroyTopic uses, keeping its
	// partition-drain safety against a still-writing producer
	topics, err := a.topicController.ListTopics(ctx)
	if err != nil {
		return err
	}
	for _, found := range topics {
		if err := a.topicController.DeleteTopic(ctx, found.Id, found.Name); err != nil {
			return err
		}
	}

	return a.systemController.DeleteSystem(ctx)
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
			return fmt.Errorf("%w: worker %s", system.ErrSystemLive, snapshot.Name)
		}
	}

	topics, err := a.topicController.ListTopics(ctx)
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
		return fmt.Errorf("%w: %s", system.ErrTopicsRegistered, strings.Join(names, ", "))
	}
	return nil
}
