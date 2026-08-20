package admin

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/cron"
	croncontroller "github.com/agentstax/vulkan/pkg/cron/controller"
	"github.com/agentstax/vulkan/pkg/migrate"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
)

// RegisterCronJob creates the job named name if it doesn't exist and returns
// it. Safe to call on every startup: schedule, data and cfg are applied on
// every call, so changing one and redeploying changes the job -- and two
// services passing different values for one name will overwrite each other.
//   - name: must not contain '*'.
//   - schedule: from cron.ParseSchedule; min rate 1m and >= cfg.Timeout.
//     A changed schedule decides when the job next runs -- a run already due
//     under the old one is dropped, not produced late.
//   - data: marshaled to the job's JSON payload
//   - cfg: may be nil or sparse
//
// A suspended job stays suspended across a call -- only SuspendCronJob and
// UnsuspendCronJob change that.
func (a *MessageAdmin) RegisterCronJob(ctx context.Context, name string, schedule *cron.Schedule, data any, cfg *croncontroller.CronJobConfig) (*cron.CronJob, error) {
	if name == "" {
		return nil, errors.New("cron job name is required")
	}

	// gate -- a cron job can't exist without the control-plane schema it rides on
	sys, err := a.systemController.Get(ctx)
	if err != nil {
		return nil, err
	}
	if sys == nil {
		return nil, migrate.ErrNotRegistered.With("cron_job", name)
	}

	// every cron_job row has exactly one owner; admin-registered jobs are the
	// system's -- they ride its lifecycle, not any one topic's
	owner, err := common.NewSystemOwner(sys.Id)
	if err != nil {
		return nil, err
	}

	return a.cronJobController.Register(ctx, owner, name, schedule, data, cfg)
}

// GetCronJob returns (nil, nil), not an error, if name isn't registered.
func (a *MessageAdmin) GetCronJob(ctx context.Context, name string) (*cron.CronJob, error) {
	if name == "" {
		return nil, errors.New("cron job name is required")
	}
	return a.cronJobController.Get(ctx, name)
}

// ListCronJobs returns every cron job, ordered by name.
func (a *MessageAdmin) ListCronJobs(ctx context.Context) ([]*cron.CronJob, error) {
	return a.cronJobController.List(ctx)
}

// SuspendCronJob stops the scheduler producing the job until unsuspended.
func (a *MessageAdmin) SuspendCronJob(ctx context.Context, name string) error {
	if name == "" {
		return errors.New("cron job name is required")
	}
	return a.cronJobController.Suspend(ctx, name)
}

// UnsuspendCronJob resumes at the schedule's next scheduled time -- one that
// came due while suspended is dropped, not produced late.
func (a *MessageAdmin) UnsuspendCronJob(ctx context.Context, name string) error {
	if name == "" {
		return errors.New("cron job name is required")
	}
	return a.cronJobController.Unsuspend(ctx, name)
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

	job, err := a.cronJobController.Get(ctx, name)
	if err != nil {
		return nil, err
	}
	if job == nil {
		return nil, cron.ErrCronJobNotFound.With("cron_job", name)
	}

	instance, err := a.jobRequestProducer.Register(ctx, cron.TopicName, topic.SchemaVersion(1))
	if err != nil {
		return nil, err
	}

	request, err := cron.NewJobRequest(job.Id, job.Name, time.Now().UTC(), job.Data, job.Metadata)
	if err != nil {
		return nil, err
	}

	compaction, err := producer.NewCompactionOptions(strconv.FormatInt(job.Id, 10), 0)
	if err != nil {
		return nil, err
	}

	// no IdempotencyKey: Produce creates a fresh v7 per call, so every produced
	// run is its own unique job.
	return instance.Produce(ctx, request, producer.ProduceOptions{
		RoutingKey: job.Name,
		Compaction: compaction,
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

	job, err := a.cronJobController.Get(ctx, name)
	if err != nil {
		return nil, err
	}
	if job == nil {
		return nil, cron.ErrCronJobNotFound.With("cron_job", name)
	}

	jobRequests, err := a.topicController.Get(ctx, cron.TopicName, topic.SchemaVersion(1))
	if err != nil {
		return nil, err
	}
	if jobRequests == nil {
		return nil, migrate.ErrNotRegistered.With("topic", cron.TopicName)
	}

	return a.cronJobController.Status(ctx, jobRequests.Id, job.Id, job.Name)
}

// CronJobRequests is the job's newest requests, one JobRequestStatus
// per (request, consumer group that receives it), newest request first.
// Requests older than the topic's retention window are gone.
// Returns ErrCronJobNotFound if name isn't registered.
func (a *MessageAdmin) CronJobRequests(ctx context.Context, name string, limit int) ([]*cron.JobRequestStatus, error) {
	if name == "" {
		return nil, errors.New("cron job name is required")
	}

	job, err := a.cronJobController.Get(ctx, name)
	if err != nil {
		return nil, err
	}
	if job == nil {
		return nil, cron.ErrCronJobNotFound.With("cron_job", name)
	}

	jobRequests, err := a.topicController.Get(ctx, cron.TopicName, topic.SchemaVersion(1))
	if err != nil {
		return nil, err
	}
	if jobRequests == nil {
		return nil, migrate.ErrNotRegistered.With("topic", cron.TopicName)
	}

	return a.cronJobController.ListRequests(ctx, jobRequests.Id, job.Id, job.Name, limit)
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

	return a.cronJobController.Delete(ctx, name)
}
