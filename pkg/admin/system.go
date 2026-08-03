package admin

import (
	"context"

	"github.com/agentstax/vulkan/pkg/alert"
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
// call it once before registering any topic. Idempotent -- a cfg matching the
// seeded row resolves as a no-op; a differing one errors with
// system.ErrSystemConfigMismatch.
//   - cfg: may be nil or sparse -- WithDefaults fills every field left unset
func (a *MessageAdmin) RegisterSystem(ctx context.Context, cfg *systemcontroller.SystemConfig) error {
	if _, err := a.systemController.RegisterSystem(ctx, cfg); err != nil {
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

	// Make sure the alert checks' cron jobs are registered.
	jobs, err := alert.Jobs()
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if err := a.ensureSystemCronJob(ctx, job); err != nil {
			return err
		}
	}
	return nil
}

// ensureSystemCronJob only creates a missing job -- not a bare idempotent
// register, whose config-mismatch check would error on a job an operator has
// altered since.
func (a *MessageAdmin) ensureSystemCronJob(ctx context.Context, job *alert.Job) error {
	existing, err := a.cronJobDatastore.GetCronJob(ctx, job.Name)
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

// AlterSystem applies cfg's non-nil fields to the singleton system config and
// returns the updated config. Returns migrate.ErrNotRegistered if
// RegisterSystem hasn't run.
//
// Two consequences:
//   - A consumer of this config snapshots it at startup, so an alter takes
//     effect on its NEXT restart, not live.
//   - A RegisterSystem call still passing the pre-alter cfg fails with
//     system.ErrSystemConfigMismatch.
func (a *MessageAdmin) AlterSystem(ctx context.Context, cfg *systemcontroller.AlterSystemConfig) (*system.System, error) {
	updated, err := a.systemController.UpdateSystem(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if updated == nil {
		return nil, migrate.ErrNotRegistered
	}
	return updated, nil
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
