package vulkan

import (
	"context"

	"github.com/agentstax/vulkan/pkg/schedule"
)

// SchedulerHandle is a schedule's name plus the client, holding no row.
// Get is the comma-ok read; every other verb returns the not-found error
// itself.
type SchedulerHandle struct {
	name   string
	client *Client
}

// Schedulers returns every registered schedule, ordered by name.
func (c *Client) Schedulers(ctx context.Context) ([]*Schedule, error) {
	return c.admin.ListSchedules(ctx)
}

// Scheduler names a schedule on the client. No I/O and no failure -- each
// verb on the handle resolves the name when called.
func (c *Client) Scheduler(name string) *SchedulerHandle {
	return &SchedulerHandle{name: name, client: c}
}

// Register declares this schedule on topicName and returns a runnable
// instance. The newest declaration wins. cfg may be nil or sparse.
func (s *SchedulerHandle) Register[Message Versioned](ctx context.Context, topicName string, cron string, payload *Message, cfg *ScheduleConfig) (*SchedulerInstance[Message], error) {
	spec := &schedule.ScheduleSpec{Name: s.name, Topic: topicName, Cron: cron}
	instance, err := s.client.scheduler.Register[Message](ctx, spec, payload, cfg)
	if err != nil {
		return nil, err
	}
	return newSchedulerInstance(s.client, instance)
}

// Get reads the schedule's row. Returns (nil, nil) when the schedule is
// not registered.
func (s *SchedulerHandle) Get(ctx context.Context) (*Schedule, error) {
	return s.client.admin.GetSchedule(ctx, s.name)
}

// Suspend stops the schedule producing until unsuspended.
func (s *SchedulerHandle) Suspend(ctx context.Context) error {
	return s.client.admin.SuspendSchedule(ctx, s.name)
}

// Unsuspend resumes at the schedule's next scheduled time -- one that came
// due while suspended is dropped, not produced late.
func (s *SchedulerHandle) Unsuspend(ctx context.Context) error {
	return s.client.admin.UnsuspendSchedule(ctx, s.name)
}

// Run produces the schedule's stored message immediately, outside its
// expression. cfg may be nil or a sparse struct.
func (s *SchedulerHandle) Run(ctx context.Context, cfg *RunScheduleConfig) (*ProduceResult[StoredMessage], error) {
	return s.client.admin.RunSchedule(ctx, s.name, cfg)
}

// Status reports the schedule's messages rolled up per consumer group.
func (s *SchedulerHandle) Status(ctx context.Context) ([]*ScheduleGroupSummary, error) {
	return s.client.admin.ScheduleStatus(ctx, s.name)
}

// ListMessages lists the schedule's produced messages, newest first.
func (s *SchedulerHandle) ListMessages(ctx context.Context, limit int) ([]*ScheduleMessageStatus, error) {
	return s.client.admin.ScheduleMessages(ctx, s.name, limit)
}

func (s *SchedulerHandle) Destroy(ctx context.Context) error {
	return s.client.admin.DestroySchedule(ctx, s.name)
}
