package vulkan

import (
	"context"
)

// Schedule is a handle: the schedule's name plus the client, holding no
// row. One handle carries both the admin verbs and the run verb; Get is
// the comma-ok read, every other verb returns the not-found error itself.
type Schedule struct {
	name   string
	client *Client
}

// Schedule names a schedule on the client. No I/O and no failure -- each
// verb on the handle resolves the name when called.
func (c *Client) Schedule(name string) *Schedule {
	return &Schedule{name: name, client: c}
}

// Get reads the schedule's row. Returns (nil, nil) when the schedule is
// not registered.
func (s *Schedule) Get(ctx context.Context) (*ScheduleData, error) {
	return s.client.admin.GetSchedule(ctx, s.name)
}

// Suspend stops the schedule producing until unsuspended.
func (s *Schedule) Suspend(ctx context.Context) error {
	return s.client.admin.SuspendSchedule(ctx, s.name)
}

// Unsuspend resumes at the schedule's next scheduled time -- one that came
// due while suspended is dropped, not produced late.
func (s *Schedule) Unsuspend(ctx context.Context) error {
	return s.client.admin.UnsuspendSchedule(ctx, s.name)
}

// Run produces the schedule's stored message immediately, outside its
// expression. cfg may be nil or a sparse struct.
func (s *Schedule) Run(ctx context.Context, cfg *RunScheduleConfig) (*ProduceResult[StoredMessage], error) {
	return s.client.admin.RunSchedule(ctx, s.name, cfg)
}

// Status reports the schedule's messages rolled up per consumer group.
func (s *Schedule) Status(ctx context.Context) ([]*GroupStatus, error) {
	return s.client.admin.ScheduleStatus(ctx, s.name)
}

// ListMessages lists the schedule's produced messages, newest first.
func (s *Schedule) ListMessages(ctx context.Context, limit int) ([]*MessageStatus, error) {
	return s.client.admin.ScheduleMessages(ctx, s.name, limit)
}

func (s *Schedule) Destroy(ctx context.Context) error {
	return s.client.admin.DestroySchedule(ctx, s.name)
}

// Schedule runs the system manager until ctx cancels -- what `vulkan
// manager run` does; the schedule producer worker produces every
// registered schedule, not just this one. A requested stop returns nil.
func (s *Schedule) Schedule(ctx context.Context) error {
	return s.client.manager.Run(ctx)
}
