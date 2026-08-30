package controller

import (
	"context"
	"errors"
	"fmt"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/schedule"
)

// Register resolves name to its schedule, creating it owned by owner if it
// doesn't exist; an existing schedule takes schedule, payload and cfg -- the newest
// declaration wins. cfg may be nil or a sparse struct -- WithDefaults fills
// every field left unset, Validate rejects what's out of range.
func (c *ScheduleController) Register(ctx context.Context, owner *common.Owner, name string, expression *schedule.Expression, payload any, cfg *ScheduleConfig) (*schedule.Schedule, error) {
	if owner == nil {
		return nil, errors.New("owner must not be nil")
	}
	if !schedule.SlugPattern.MatchString(name) {
		return nil, fmt.Errorf("name must match %s, got %q", schedule.SlugPattern, name)
	}
	if expression == nil {
		return nil, errors.New("expression is required")
	}
	if cfg == nil {
		cfg = &ScheduleConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if cfg.Timeout > expression.MinRate() {
		return nil, fmt.Errorf("timeout %v exceeds expression %q's min rate %v", cfg.Timeout, expression, expression.MinRate())
	}

	registered, err := c.datastore.Register(ctx, owner, toRegisterScheduleData(name, expression, payload, cfg))
	if err != nil {
		return nil, err
	}
	return toSchedule(registered)
}

// Get returns (nil, nil) if name isn't registered.
func (c *ScheduleController) Get(ctx context.Context, name string) (*schedule.Schedule, error) {
	if name == "" {
		return nil, errors.New("name is required")
	}

	found, err := c.datastore.Get(ctx, name)
	if err != nil || found == nil {
		return nil, err
	}
	return toSchedule(found)
}

// List returns every schedule, ordered by name.
func (c *ScheduleController) List(ctx context.Context) ([]*schedule.Schedule, error) {
	listed, err := c.datastore.List(ctx)
	if err != nil {
		return nil, err
	}

	var schedules []*schedule.Schedule
	for _, data := range listed {
		found, err := toSchedule(&data)
		if err != nil {
			return nil, err
		}
		schedules = append(schedules, found)
	}
	return schedules, nil
}

// Suspend stops the scheduler producing the schedule until unsuspended.
// Returns schedule.ErrScheduleNotFound if name isn't registered.
func (c *ScheduleController) Suspend(ctx context.Context, name string) error {
	if name == "" {
		return errors.New("name is required")
	}
	return c.datastore.Suspend(ctx, name)
}

// Unsuspend resumes at the schedule's next scheduled time -- one that
// came due while suspended is dropped, not produced late.
// Returns schedule.ErrScheduleNotFound if name isn't registered.
func (c *ScheduleController) Unsuspend(ctx context.Context, name string) error {
	if name == "" {
		return errors.New("name is required")
	}
	return c.datastore.Unsuspend(ctx, name)
}

// Delete permanently deletes the schedule.
// Returns schedule.ErrScheduleNotFound if name isn't registered.
func (c *ScheduleController) Delete(ctx context.Context, name string) error {
	if name == "" {
		return errors.New("name is required")
	}
	return c.datastore.Delete(ctx, name)
}

// ListRequests is the schedule's requests, one JobRequestStatus
// per (request, consumer group that receives it), newest request first.
// Requests older than the topic's retention window are gone.
func (c *ScheduleController) ListRequests(ctx context.Context, jobRequestsTopicId int64, scheduleId int64, name string, limit int) ([]*schedule.JobRequestStatus, error) {
	if jobRequestsTopicId <= 0 {
		return nil, fmt.Errorf("jobRequestsTopicId must be > 0, got %d", jobRequestsTopicId)
	}
	if scheduleId <= 0 {
		return nil, fmt.Errorf("scheduleId must be > 0, got %d", scheduleId)
	}
	if name == "" {
		return nil, errors.New("name is required")
	}
	if limit <= 0 {
		return nil, fmt.Errorf("limit must be > 0, got %d", limit)
	}

	listed, err := c.datastore.ListRequests(ctx, jobRequestsTopicId, scheduleId, name, limit)
	if err != nil {
		return nil, err
	}

	var requests []*schedule.JobRequestStatus
	for _, data := range listed {
		request, err := toJobRequestStatus(&data)
		if err != nil {
			return nil, err
		}
		requests = append(requests, request)
	}
	return requests, nil
}

// Status is one GroupStatus per consumer group that receives the
// schedule's requests. Counts cover the topic's retention window.
func (c *ScheduleController) Status(ctx context.Context, jobRequestsTopicId int64, scheduleId int64, name string) ([]*schedule.GroupStatus, error) {
	if jobRequestsTopicId <= 0 {
		return nil, fmt.Errorf("jobRequestsTopicId must be > 0, got %d", jobRequestsTopicId)
	}
	if scheduleId <= 0 {
		return nil, fmt.Errorf("scheduleId must be > 0, got %d", scheduleId)
	}
	if name == "" {
		return nil, errors.New("name is required")
	}

	listed, err := c.datastore.Status(ctx, jobRequestsTopicId, scheduleId, name)
	if err != nil {
		return nil, err
	}

	var statuses []*schedule.GroupStatus
	for _, data := range listed {
		statuses = append(statuses, toGroupStatus(&data))
	}
	return statuses, nil
}
