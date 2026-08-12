package admin

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/cron"
	croncontroller "github.com/agentstax/vulkan/pkg/cron/controller"
	"github.com/agentstax/vulkan/pkg/migrate"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
)

// RegisterCronJob is idempotent -- an existing name with an identical
// schedule/data/cfg resolves to its job; a differing one errors with
// ErrCronJobConfigMismatch, so two callers disagreeing about the schedule
// can't silently share a job.
//   - name: must not contain '*'.
//   - schedule: from cron.ParseSchedule; min rate 1m and >= cfg.Timeout
//   - data: marshaled to the job's JSON payload
//   - cfg: may be nil or sparse
func (a *MessageAdmin) RegisterCronJob(ctx context.Context, name string, schedule *cron.Schedule, data any, cfg *croncontroller.CronJobConfig) (*cron.CronJob, error) {
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

	// every cron_job row has exactly one owner; admin-registered jobs are the
	// system's -- they ride its lifecycle, not any one topic's
	owner, err := common.NewSystemOwner(sys.Id)
	if err != nil {
		return nil, err
	}

	return a.cronJobController.RegisterCronJob(ctx, owner, name, schedule, data, cfg)
}

// AlterCronJob applies cfg's set fields to the named job and returns the
// updated job. Returns ErrCronJobNotFound if name isn't registered.
//
// Two consequences to plan around:
//   - A schedule change re-seeds next_scheduled_time from the new schedule --
//     a scheduled time already due under the old one is dropped.
//   - RegisterCronJob calls still passing the pre-alter config will fail with
//     ErrCronJobConfigMismatch -- deliberate, so declarative register calls
//     can't silently drift from what an operator changed.
func (a *MessageAdmin) AlterCronJob(ctx context.Context, name string, cfg *croncontroller.AlterCronJobConfig) (*cron.CronJob, error) {
	if name == "" {
		return nil, errors.New("cron job name is required")
	}

	updated, err := a.cronJobController.UpdateCronJob(ctx, name, cfg)
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
	return a.cronJobController.GetCronJob(ctx, name)
}

// ListCronJobs returns every cron job, ordered by name.
func (a *MessageAdmin) ListCronJobs(ctx context.Context) ([]*cron.CronJob, error) {
	return a.cronJobController.ListCronJobs(ctx)
}

// SuspendCronJob stops the scheduler producing the job until unsuspended.
func (a *MessageAdmin) SuspendCronJob(ctx context.Context, name string) error {
	if name == "" {
		return errors.New("cron job name is required")
	}
	return a.cronJobController.SuspendCronJob(ctx, name)
}

// UnsuspendCronJob resumes at the schedule's next scheduled time -- one that
// came due while suspended is dropped, not produced late.
func (a *MessageAdmin) UnsuspendCronJob(ctx context.Context, name string) error {
	if name == "" {
		return errors.New("cron job name is required")
	}
	return a.cronJobController.UnsuspendCronJob(ctx, name)
}

// RunCronJob produces one JobRequest for the named job immediately, outside
// its schedule -- the schedule and next scheduled time are untouched, and a
// suspended job still runs.
// cfg may be nil or a sparse struct.
// Returns ErrCronJobNotFound if name isn't registered.
//
// Two deliberate consequences:
//   - The request's concurrency is cfg.Concurrency, NOT the job's own policy
//     -- by default 'allow', so it runs even while a previous request is still
//     running.
//   - It supersedes a pending JobRequest no consumer has claimed yet.
func (a *MessageAdmin) RunCronJob(ctx context.Context, name string, cfg *RunCronJobConfig) (*producer.ProduceResult[cron.JobRequest], error) {
	if name == "" {
		return nil, errors.New("cron job name is required")
	}
	if cfg == nil {
		cfg = &RunCronJobConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	job, err := a.cronJobController.GetCronJob(ctx, name)
	if err != nil {
		return nil, err
	}
	if job == nil {
		return nil, fmt.Errorf("%w: %s", cron.ErrCronJobNotFound, name)
	}

	instance, err := a.jobRequestProducer.Register(ctx, cron.TopicName, topic.SchemaVersion(1))
	if err != nil {
		return nil, err
	}

	request, err := cron.NewJobRequest(job.Id, job.Name, time.Now().UTC(), job.Data, job.Metadata)
	if err != nil {
		return nil, err
	}

	// no IdempotencyKey: Produce creates a fresh v7 per call, so every produced
	// run is its own unique job.
	return instance.Produce(ctx, request, producer.ProduceOptions{
		RoutingKey:    job.Name,
		CompactionKey: strconv.FormatInt(job.Id, 10),
		Message: &common.MessageOptions{
			Concurrency: cfg.Concurrency,
			Timeout:     job.Timeout,
		},
	})
}

// CronJobStatus is one GroupStatus per consumer group that receives the
// job's requests. Counts cover the topic's retention window.
// Returns ErrCronJobNotFound if name isn't registered.
func (a *MessageAdmin) CronJobStatus(ctx context.Context, name string) ([]*cron.GroupStatus, error) {
	if name == "" {
		return nil, errors.New("cron job name is required")
	}

	job, err := a.cronJobController.GetCronJob(ctx, name)
	if err != nil {
		return nil, err
	}
	if job == nil {
		return nil, fmt.Errorf("%w: %s", cron.ErrCronJobNotFound, name)
	}

	jobRequests, err := a.topicController.GetTopic(ctx, cron.TopicName, topic.SchemaVersion(1))
	if err != nil {
		return nil, err
	}
	if jobRequests == nil {
		return nil, fmt.Errorf("topic %q not found -- register the system with RegisterSystem first: %w", cron.TopicName, migrate.ErrNotRegistered)
	}

	return a.cronJobController.CronJobStatus(ctx, jobRequests.Id, job.Id, job.Name)
}

// CronJobRequests is the job's newest requests, one JobRequestStatus
// per (request, consumer group that receives it), newest request first.
// Requests older than the topic's retention window are gone.
// Returns ErrCronJobNotFound if name isn't registered.
func (a *MessageAdmin) CronJobRequests(ctx context.Context, name string, limit int) ([]*cron.JobRequestStatus, error) {
	if name == "" {
		return nil, errors.New("cron job name is required")
	}

	job, err := a.cronJobController.GetCronJob(ctx, name)
	if err != nil {
		return nil, err
	}
	if job == nil {
		return nil, fmt.Errorf("%w: %s", cron.ErrCronJobNotFound, name)
	}

	jobRequests, err := a.topicController.GetTopic(ctx, cron.TopicName, topic.SchemaVersion(1))
	if err != nil {
		return nil, err
	}
	if jobRequests == nil {
		return nil, fmt.Errorf("topic %q not found -- register the system with RegisterSystem first: %w", cron.TopicName, migrate.ErrNotRegistered)
	}

	return a.cronJobController.CronJobRequests(ctx, jobRequests.Id, job.Id, job.Name, limit)
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
	return a.cronJobController.DeleteCronJob(ctx, name)
}
