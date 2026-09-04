package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/schedule"
)

// Register resolves name to its schedule, creating it under
// systemId if it doesn't exist; the newest declaration wins. The payload is
// stored marshaled with Message's schema version; every produce carries both.
func (c *ScheduleController) Register[Message common.Versioned](ctx context.Context, systemId int64, name string, cron string, topicId int64, payload *Message, timeout time.Duration, concurrency common.ConcurrencyPolicy, metadata any) (*schedule.Schedule, error) {
	if systemId <= 0 {
		return nil, fmt.Errorf("systemId must be > 0, got %d", systemId)
	}
	if !schedule.SlugPattern.MatchString(name) {
		return nil, fmt.Errorf("name must match %s, got %q", schedule.SlugPattern, name)
	}
	if cron == "" {
		return nil, errors.New("cron is required")
	}
	if topicId <= 0 {
		return nil, fmt.Errorf("topicId must be > 0, got %d", topicId)
	}
	if payload == nil {
		return nil, errors.New("payload must not be nil")
	}
	if timeout <= 0 {
		return nil, fmt.Errorf("timeout must be > 0, got %v", timeout)
	}
	if err := concurrency.Validate(); err != nil {
		return nil, fmt.Errorf("concurrency: %w", err)
	}
	expression, err := schedule.ParseExpression(cron)
	if err != nil {
		return nil, err
	}
	if timeout > expression.MinRate() {
		return nil, fmt.Errorf("timeout %v exceeds cron %q's min rate %v", timeout, cron, expression.MinRate())
	}

	registered, err := c.datastore.Register(ctx, systemId, topicId, name, expression, concurrency, timeout, payload, common.SchemaVersionOf[Message](), metadata)
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

// ListMessages is the schedule's messages on its target topic, one
// ScheduleMessageStatus per (message, consumer group that receives it), newest
// message first. Messages older than the topic's retention window are gone.
func (c *ScheduleController) ListMessages(ctx context.Context, topicId int64, name string, limit int) ([]*schedule.ScheduleMessageStatus, error) {
	if topicId <= 0 {
		return nil, fmt.Errorf("topicId must be > 0, got %d", topicId)
	}
	if name == "" {
		return nil, errors.New("name is required")
	}
	if limit <= 0 {
		return nil, fmt.Errorf("limit must be > 0, got %d", limit)
	}

	listed, err := c.datastore.ListMessages(ctx, topicId, name, limit)
	if err != nil {
		return nil, err
	}

	var messages []*schedule.ScheduleMessageStatus
	for _, data := range listed {
		messages = append(messages, toScheduleMessageStatus(&data))
	}
	return messages, nil
}

// Status is one ScheduleGroupSummary per consumer group that receives the
// schedule's messages. Counts cover the topic's retention window.
func (c *ScheduleController) Status(ctx context.Context, topicId int64, name string) ([]*schedule.ScheduleGroupSummary, error) {
	if topicId <= 0 {
		return nil, fmt.Errorf("topicId must be > 0, got %d", topicId)
	}
	if name == "" {
		return nil, errors.New("name is required")
	}

	listed, err := c.datastore.Status(ctx, topicId, name)
	if err != nil {
		return nil, err
	}

	var statuses []*schedule.ScheduleGroupSummary
	for _, data := range listed {
		statuses = append(statuses, toScheduleGroupSummary(&data))
	}
	return statuses, nil
}
