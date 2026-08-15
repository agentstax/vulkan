package controller

import (
	"context"
	"errors"
	"fmt"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/cron"
)

// RegisterCronJob resolves name to its job, creating it owned by owner if it
// doesn't exist; an existing job takes schedule, data and cfg -- the newest
// declaration wins. cfg may be nil or a sparse struct -- WithDefaults fills
// every field left unset, Validate rejects what's out of range.
func (c *CronJobController) RegisterCronJob(ctx context.Context, owner *common.Owner, name string, schedule *cron.Schedule, data any, cfg *CronJobConfig) (*cron.CronJob, error) {
	if owner == nil {
		return nil, errors.New("owner must not be nil")
	}
	if !cron.SlugPattern.MatchString(name) {
		return nil, fmt.Errorf("name must match %s, got %q", cron.SlugPattern, name)
	}
	if schedule == nil {
		return nil, errors.New("schedule is required")
	}
	if cfg == nil {
		cfg = &CronJobConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if cfg.Timeout > schedule.MinRate() {
		return nil, fmt.Errorf("timeout %v exceeds schedule %q's min rate %v", cfg.Timeout, schedule, schedule.MinRate())
	}

	registered, err := c.datastore.RegisterCronJob(ctx, owner, toRegisterCronJobData(name, schedule, data, cfg))
	if err != nil {
		return nil, err
	}
	return toCronJob(registered)
}

// GetCronJob returns (nil, nil) if name isn't registered.
func (c *CronJobController) GetCronJob(ctx context.Context, name string) (*cron.CronJob, error) {
	if name == "" {
		return nil, errors.New("name is required")
	}

	found, err := c.datastore.GetCronJob(ctx, name)
	if err != nil || found == nil {
		return nil, err
	}
	return toCronJob(found)
}

// ListCronJobs returns every cron job, ordered by name.
func (c *CronJobController) ListCronJobs(ctx context.Context) ([]*cron.CronJob, error) {
	listed, err := c.datastore.ListCronJobs(ctx)
	if err != nil {
		return nil, err
	}

	var jobs []*cron.CronJob
	for _, data := range listed {
		job, err := toCronJob(data)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}

// SuspendCronJob stops the scheduler producing the job until unsuspended.
// Returns cron.ErrCronJobNotFound if name isn't registered.
func (c *CronJobController) SuspendCronJob(ctx context.Context, name string) error {
	if name == "" {
		return errors.New("name is required")
	}
	return c.datastore.SuspendCronJob(ctx, name)
}

// UnsuspendCronJob resumes at the schedule's next scheduled time -- one that
// came due while suspended is dropped, not produced late.
// Returns cron.ErrCronJobNotFound if name isn't registered.
func (c *CronJobController) UnsuspendCronJob(ctx context.Context, name string) error {
	if name == "" {
		return errors.New("name is required")
	}
	return c.datastore.UnsuspendCronJob(ctx, name)
}

// DeleteCronJob permanently deletes the job.
// Returns cron.ErrCronJobNotFound if name isn't registered.
func (c *CronJobController) DeleteCronJob(ctx context.Context, name string) error {
	if name == "" {
		return errors.New("name is required")
	}
	return c.datastore.DeleteCronJob(ctx, name)
}

// CronJobRequests is the job's requests, one JobRequestStatus
// per (request, consumer group that receives it), newest request first.
// Requests older than the topic's retention window are gone.
func (c *CronJobController) CronJobRequests(ctx context.Context, jobRequestsTopicId int64, cronJobId int64, name string, limit int) ([]*cron.JobRequestStatus, error) {
	if jobRequestsTopicId <= 0 {
		return nil, fmt.Errorf("jobRequestsTopicId must be > 0, got %d", jobRequestsTopicId)
	}
	if cronJobId <= 0 {
		return nil, fmt.Errorf("cronJobId must be > 0, got %d", cronJobId)
	}
	if name == "" {
		return nil, errors.New("name is required")
	}
	if limit <= 0 {
		return nil, fmt.Errorf("limit must be > 0, got %d", limit)
	}

	listed, err := c.datastore.CronJobRequests(ctx, jobRequestsTopicId, cronJobId, name, limit)
	if err != nil {
		return nil, err
	}

	var requests []*cron.JobRequestStatus
	for _, data := range listed {
		request, err := toJobRequestStatus(data)
		if err != nil {
			return nil, err
		}
		requests = append(requests, request)
	}
	return requests, nil
}

// CronJobStatus is one GroupStatus per consumer group that receives the
// job's requests. Counts cover the topic's retention window.
func (c *CronJobController) CronJobStatus(ctx context.Context, jobRequestsTopicId int64, cronJobId int64, name string) ([]*cron.GroupStatus, error) {
	if jobRequestsTopicId <= 0 {
		return nil, fmt.Errorf("jobRequestsTopicId must be > 0, got %d", jobRequestsTopicId)
	}
	if cronJobId <= 0 {
		return nil, fmt.Errorf("cronJobId must be > 0, got %d", cronJobId)
	}
	if name == "" {
		return nil, errors.New("name is required")
	}

	listed, err := c.datastore.CronJobStatus(ctx, jobRequestsTopicId, cronJobId, name)
	if err != nil {
		return nil, err
	}

	var statuses []*cron.GroupStatus
	for _, data := range listed {
		statuses = append(statuses, toGroupStatus(data))
	}
	return statuses, nil
}
