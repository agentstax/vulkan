package controller

import (
	"context"
	"errors"
	"fmt"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/schedule"
)

// Register resolves spec.Name to its schedule, creating it under systemId if
// it doesn't exist; an existing schedule takes spec.Cron, topic, payload and
// cfg -- the newest declaration wins. The payload is stored marshaled with
// Message's schema version; every produce carries both. cfg may be nil or a
// sparse struct -- WithDefaults fills every field left unset, Validate
// rejects what's out of range.
func (c *ScheduleController) Register[Message common.Versioned](ctx context.Context, systemId int64, spec *schedule.ScheduleSpec, topicId int64, payload *Message, cfg *schedule.ScheduleConfig) (*schedule.ScheduleData, error) {
	if systemId <= 0 {
		return nil, fmt.Errorf("systemId must be > 0, got %d", systemId)
	}
	if spec == nil {
		return nil, errors.New("spec must not be nil")
	}
	if !schedule.SlugPattern.MatchString(spec.Name) {
		return nil, fmt.Errorf("Name must match %s, got %q", schedule.SlugPattern, spec.Name)
	}
	if spec.Topic == "" {
		return nil, errors.New("Topic is required")
	}
	if spec.Cron == "" {
		return nil, errors.New("Cron is required")
	}
	if topicId <= 0 {
		return nil, fmt.Errorf("topicId must be > 0, got %d", topicId)
	}
	if payload == nil {
		return nil, errors.New("payload must not be nil")
	}
	if cfg == nil {
		cfg = &schedule.ScheduleConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	expression, err := schedule.ParseExpression(spec.Cron)
	if err != nil {
		return nil, err
	}
	if cfg.Timeout > expression.MinRate() {
		return nil, fmt.Errorf("Timeout %v exceeds Cron %q's min rate %v", cfg.Timeout, spec.Cron, expression.MinRate())
	}

	registered, err := c.datastore.Register(ctx, toScheduleDeclaration(systemId, spec.Name, expression, topicId, payload, cfg))
	if err != nil {
		return nil, err
	}
	return toScheduleData(registered)
}

// Get returns (nil, nil) if name isn't registered.
func (c *ScheduleController) Get(ctx context.Context, name string) (*schedule.ScheduleData, error) {
	if name == "" {
		return nil, errors.New("name is required")
	}

	found, err := c.datastore.Get(ctx, name)
	if err != nil || found == nil {
		return nil, err
	}
	return toScheduleData(found)
}

// List returns every schedule, ordered by name.
func (c *ScheduleController) List(ctx context.Context) ([]*schedule.ScheduleData, error) {
	listed, err := c.datastore.List(ctx)
	if err != nil {
		return nil, err
	}

	var schedules []*schedule.ScheduleData
	for _, data := range listed {
		found, err := toScheduleData(&data)
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
// MessageStatus per (message, consumer group that receives it), newest
// message first. Messages older than the topic's retention window are gone.
func (c *ScheduleController) ListMessages(ctx context.Context, topicId int64, name string, limit int) ([]*schedule.MessageStatus, error) {
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

	var messages []*schedule.MessageStatus
	for _, data := range listed {
		messages = append(messages, toMessageStatus(&data))
	}
	return messages, nil
}

// Status is one GroupStatus per consumer group that receives the
// schedule's messages. Counts cover the topic's retention window.
func (c *ScheduleController) Status(ctx context.Context, topicId int64, name string) ([]*schedule.GroupStatus, error) {
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

	var statuses []*schedule.GroupStatus
	for _, data := range listed {
		statuses = append(statuses, toGroupStatus(&data))
	}
	return statuses, nil
}
