package admin

import (
	"context"
	"errors"
	"fmt"

	"github.com/agentstax/vulkan/pkg/cron"
	"github.com/agentstax/vulkan/pkg/migrate"
)

// RegisterCronJob is idempotent -- an existing name with an identical
// schedule/data/cfg resolves to its job; a differing one errors with
// ErrCronJobConfigMismatch, so two callers disagreeing about the schedule
// can't silently share a job.
//   - name: must not contain '*'.
//   - schedule: from cron.ParseSchedule; min firing rate 1m and >= cfg.Timeout
//   - data: marshaled to the job's JSON payload
//   - cfg: may be nil or sparse
func (a *MessageAdmin) RegisterCronJob(ctx context.Context, name string, schedule *cron.Schedule, data any, cfg *cron.Config) (*cron.CronJob, error) {
	if name == "" {
		return nil, errors.New("cron job name is required")
	}

	// gate -- a cron job can't exist without the control-plane schema it rides on
	sys, err := a.systemController.GetSystem(ctx)
	if err != nil {
		return nil, err
	}
	if sys == nil {
		return nil, fmt.Errorf("register the system with RegisterSystem before registering cron job %q: %w", name, migrate.ErrNotRegistered)
	}

	if cfg == nil {
		cfg = &cron.Config{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return a.cronJobDatastore.RegisterCronJob(ctx, name, schedule, data, *cfg)
}

// AlterCronJob applies cfg's set fields to the named job and returns the
// updated job. Returns ErrCronJobNotFound if name isn't registered.
//
// Two consequences to plan around:
//   - A schedule change re-seeds next_scheduled_time from the new schedule's
//     next firing -- one already due under the old schedule is dropped.
//   - RegisterCronJob calls still passing the pre-alter config will fail with
//     ErrCronJobConfigMismatch -- deliberate, so declarative register calls
//     can't silently drift from what an operator changed.
func (a *MessageAdmin) AlterCronJob(ctx context.Context, name string, cfg *cron.AlterConfig) (*cron.CronJob, error) {
	if name == "" {
		return nil, errors.New("cron job name is required")
	}

	if cfg == nil {
		cfg = &cron.AlterConfig{}
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	updated, err := a.cronJobDatastore.UpdateCronJob(ctx, name, cfg)
	if err != nil {
		return nil, err
	}
	if updated == nil {
		return nil, fmt.Errorf("%w: %s", cron.ErrCronJobNotFound, name)
	}
	return updated, nil
}

// GetCronJob returns (nil, nil), not an error, if name isn't registered.
func (a *MessageAdmin) GetCronJob(ctx context.Context, name string) (*cron.CronJob, error) {
	if name == "" {
		return nil, errors.New("cron job name is required")
	}
	return a.cronJobDatastore.GetCronJob(ctx, name)
}

// ListCronJobs returns every cron job, ordered by name.
func (a *MessageAdmin) ListCronJobs(ctx context.Context) ([]*cron.CronJob, error) {
	return a.cronJobDatastore.ListCronJobs(ctx)
}

// SuspendCronJob stops the job from firing until unsuspended.
func (a *MessageAdmin) SuspendCronJob(ctx context.Context, name string) error {
	if name == "" {
		return errors.New("cron job name is required")
	}
	return a.cronJobDatastore.SuspendCronJob(ctx, name)
}

// UnsuspendCronJob resumes at the schedule's next firing -- one that came due
// while suspended is dropped, not fired late.
func (a *MessageAdmin) UnsuspendCronJob(ctx context.Context, name string) error {
	if name == "" {
		return errors.New("cron job name is required")
	}
	return a.cronJobDatastore.UnsuspendCronJob(ctx, name)
}

// DestroyCronJob permanently deletes the job. Returns ErrDestroyDisabled
// unless MessageAdminConfig.AllowDestroy is set.
func (a *MessageAdmin) DestroyCronJob(ctx context.Context, name string) error {
	if !a.allowDestroy {
		return ErrDestroyDisabled
	}
	if name == "" {
		return errors.New("cron job name is required")
	}
	return a.cronJobDatastore.DestroyCronJob(ctx, name)
}
