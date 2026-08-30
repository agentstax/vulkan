package admin

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/migrate"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/schedule"
	schedulecontroller "github.com/agentstax/vulkan/pkg/schedule/controller"
	"github.com/agentstax/vulkan/pkg/topic"
)

// RegisterSchedule creates the schedule named name if it doesn't exist and returns
// it. Safe to call on every startup: schedule, payload and cfg are applied on
// every call, so changing one and redeploying changes the schedule -- and two
// services passing different values for one name will overwrite each other.
//   - name: must not contain '*'.
//   - schedule: from schedule.ParseExpression; min rate 1m and >= cfg.Timeout.
//     A changed schedule decides when the schedule next runs -- a run already due
//     under the old one is dropped, not produced late.
//   - data: marshaled to the schedule's JSON payload
//   - cfg: may be nil or sparse
//
// A suspended schedule stays suspended across a call -- only SuspendSchedule and
// UnsuspendSchedule change that.
func (a *MessageAdmin) RegisterSchedule(ctx context.Context, name string, expression *schedule.Expression, payload any, cfg *schedulecontroller.ScheduleConfig) (*schedule.Schedule, error) {
	if name == "" {
		return nil, errors.New("schedule name is required")
	}

	// gate -- a schedule can't exist without the control-plane schema it rides on
	sys, err := a.systemController.Get(ctx)
	if err != nil {
		return nil, err
	}
	if sys == nil {
		return nil, migrate.ErrNotRegistered.With("schedule", name)
	}

	// every schedule row has exactly one owner; admin-registered schedules are the
	// system's -- they ride its lifecycle, not any one topic's
	owner, err := common.NewSystemOwner(sys.Id)
	if err != nil {
		return nil, err
	}

	return a.scheduleController.Register(ctx, owner, name, expression, payload, cfg)
}

// GetSchedule returns (nil, nil), not an error, if name isn't registered.
func (a *MessageAdmin) GetSchedule(ctx context.Context, name string) (*schedule.Schedule, error) {
	if name == "" {
		return nil, errors.New("schedule name is required")
	}
	return a.scheduleController.Get(ctx, name)
}

// ListSchedules returns every schedule, ordered by name.
func (a *MessageAdmin) ListSchedules(ctx context.Context) ([]*schedule.Schedule, error) {
	return a.scheduleController.List(ctx)
}

// SuspendSchedule stops the scheduler producing the schedule until unsuspended.
func (a *MessageAdmin) SuspendSchedule(ctx context.Context, name string) error {
	if name == "" {
		return errors.New("schedule name is required")
	}
	return a.scheduleController.Suspend(ctx, name)
}

// UnsuspendSchedule resumes at the schedule's next scheduled time -- one that
// came due while suspended is dropped, not produced late.
func (a *MessageAdmin) UnsuspendSchedule(ctx context.Context, name string) error {
	if name == "" {
		return errors.New("schedule name is required")
	}
	return a.scheduleController.Unsuspend(ctx, name)
}

// RunSchedule produces one JobRequest for the named schedule immediately, outside
// its schedule -- the schedule and next scheduled time are untouched, and a
// suspended schedule still runs.
// cfg may be nil or a sparse struct.
// Returns ErrScheduleNotFound if name isn't registered.
//
// Two deliberate consequences:
//   - The request's concurrency is cfg.Concurrency, NOT the schedule's own policy
//     -- by default 'parallel', so it runs even while a previous request is still
//     running.
//   - It supersedes a pending JobRequest no consumer has claimed yet.
func (a *MessageAdmin) RunSchedule(ctx context.Context, name string, cfg *RunScheduleConfig) (*producer.ProduceResult[schedule.JobRequest], error) {
	if name == "" {
		return nil, errors.New("schedule name is required")
	}
	if cfg == nil {
		cfg = &RunScheduleConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	found, err := a.scheduleController.Get(ctx, name)
	if err != nil {
		return nil, err
	}
	if found == nil {
		return nil, schedule.ErrScheduleNotFound.With("schedule", name)
	}

	instance, err := a.jobRequestProducer.Register[schedule.JobRequest](ctx, schedule.TopicName)
	if err != nil {
		return nil, err
	}

	request, err := schedule.NewJobRequest(found.Id, found.Name, time.Now().UTC(), found.Payload, found.Metadata)
	if err != nil {
		return nil, err
	}

	compaction, err := producer.NewCompactionOptions(0)
	if err != nil {
		return nil, err
	}

	// no IdempotencyKey: Produce creates a fresh v7 per call, so every produced
	// run is its own unique schedule.
	return instance.Produce(ctx, request, producer.ProduceOptions{
		RoutingKey: found.Name,
		MessageKey: strconv.FormatInt(found.Id, 10),
		Compaction: compaction,
		Message: &common.MessageOptions{
			Concurrency: cfg.Concurrency,
			Timeout:     found.Timeout,
		},
	})
}

// ScheduleStatus is one GroupStatus per consumer group that receives the
// schedule's requests. Counts cover the topic's retention window.
// Returns ErrScheduleNotFound if name isn't registered.
func (a *MessageAdmin) ScheduleStatus(ctx context.Context, name string) ([]*schedule.GroupStatus, error) {
	if name == "" {
		return nil, errors.New("schedule name is required")
	}

	found, err := a.scheduleController.Get(ctx, name)
	if err != nil {
		return nil, err
	}
	if found == nil {
		return nil, schedule.ErrScheduleNotFound.With("schedule", name)
	}

	jobRequests, err := a.topicController.Get(ctx, schedule.TopicName)
	if err != nil {
		return nil, err
	}
	if jobRequests == nil {
		return nil, migrate.ErrNotRegistered.With("topic", schedule.TopicName)
	}

	return a.scheduleController.Status(ctx, jobRequests.Id, found.Id, found.Name)
}

// ScheduleRequests is the schedule's newest requests, one JobRequestStatus
// per (request, consumer group that receives it), newest request first.
// Requests older than the topic's retention window are gone.
// Returns ErrScheduleNotFound if name isn't registered.
func (a *MessageAdmin) ScheduleRequests(ctx context.Context, name string, limit int) ([]*schedule.JobRequestStatus, error) {
	if name == "" {
		return nil, errors.New("schedule name is required")
	}

	found, err := a.scheduleController.Get(ctx, name)
	if err != nil {
		return nil, err
	}
	if found == nil {
		return nil, schedule.ErrScheduleNotFound.With("schedule", name)
	}

	jobRequests, err := a.topicController.Get(ctx, schedule.TopicName)
	if err != nil {
		return nil, err
	}
	if jobRequests == nil {
		return nil, migrate.ErrNotRegistered.With("topic", schedule.TopicName)
	}

	return a.scheduleController.ListRequests(ctx, jobRequests.Id, found.Id, found.Name, limit)
}

// DestroySchedule permanently deletes the schedule. Returns topic.ErrDestroyDisabled
// unless MessageAdminConfig.AllowDestroy is set.
func (a *MessageAdmin) DestroySchedule(ctx context.Context, name string) error {
	if !a.allowDestroy {
		return topic.ErrDestroyDisabled
	}
	if name == "" {
		return errors.New("schedule name is required")
	}

	return a.scheduleController.Delete(ctx, name)
}
