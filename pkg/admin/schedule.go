package admin

import (
	"context"
	"errors"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/migrate"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/schedule"
	schedulecontroller "github.com/agentstax/vulkan/pkg/schedule/controller"
	"github.com/agentstax/vulkan/pkg/topic"
)

// RegisterSchedule declares the schedule named name on the target topic and
// returns it. Safe to call on every startup: the newest declaration wins, so
// two services passing different values for one name overwrite each other.
// A changed expression drops a time already due under the old one; a
// suspended schedule stays suspended. The name is the message key of every
// produce. cfg may be nil or sparse.
func (a *MessageAdmin) RegisterSchedule[Message topic.Versioned](ctx context.Context, name string, expression *schedule.Expression, topicName string, payload *Message, cfg *schedulecontroller.ScheduleConfig) (*schedule.Schedule, error) {
	if name == "" {
		return nil, errors.New("schedule name is required")
	}
	if topicName == "" {
		return nil, errors.New("topic name is required")
	}

	// gate -- a schedule needs the control-plane schema RegisterSystem creates
	sys, err := a.systemController.Get(ctx)
	if err != nil {
		return nil, err
	}
	if sys == nil {
		return nil, migrate.ErrNotRegistered.With("schedule", name)
	}

	target, err := a.topicController.Get(ctx, topicName)
	if err != nil {
		return nil, err
	}
	if target == nil {
		return nil, topic.ErrTopicNotFound.With("topic", topicName)
	}

	if target.DeliveryLogMode != topic.DeliveryLogModeAll {
		a.Logger.WarnContext(ctx, schedule.EventTargetKeepsNoSuccessRows.Message, "code", schedule.EventTargetKeepsNoSuccessRows.Code, "schedule", name, "topic", target.Name, "delivery_log_mode", string(target.DeliveryLogMode))
	}

	return a.scheduleController.Register(ctx, sys.Id, name, expression, target.Id, payload, cfg)
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

// RunSchedule produces the named schedule's stored message immediately,
// outside its expression -- the expression and next scheduled time are
// untouched, and a suspended schedule still runs.
// cfg may be nil or a sparse struct.
// Returns ErrScheduleNotFound if name isn't registered.
//
// Two deliberate consequences:
//   - The message's concurrency is cfg.Concurrency, NOT the schedule's own
//     policy -- by default 'parallel', so it runs even while a previous
//     message is still running.
//   - It supersedes a pending message no consumer has claimed yet.
func (a *MessageAdmin) RunSchedule(ctx context.Context, name string, cfg *RunScheduleConfig) (*producer.ProduceResult[schedule.StoredMessage], error) {
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

	target, err := a.topicController.GetById(ctx, found.TopicId)
	if err != nil {
		return nil, err
	}
	if target == nil {
		return nil, topic.ErrTopicNotFound.With("topic_id", found.TopicId)
	}
	instance, err := a.scheduleProducer.Register[schedule.StoredMessage](ctx, target.Name)
	if err != nil {
		return nil, err
	}

	stored, err := schedule.NewStoredMessage(found.Payload, found.SchemaVersion)
	if err != nil {
		return nil, err
	}

	compaction, err := producer.NewCompactionOptions(0)
	if err != nil {
		return nil, err
	}

	// no IdempotencyKey: Produce creates a fresh v7 per call, so every run is
	// its own message
	return instance.Produce(ctx, stored, producer.ProduceOptions{
		RoutingKey: found.Name,
		MessageKey: found.Name,
		Compaction: compaction,
		Message: &common.MessageOptions{
			Concurrency: cfg.Concurrency,
			Timeout:     found.Timeout,
			ScheduledAt: time.Now().UTC(),
		},
	})
}

// ScheduleStatus is one GroupStatus per consumer group that receives the
// schedule's messages. Counts cover the target topic's retention window.
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

	return a.scheduleController.Status(ctx, found.TopicId, found.Name)
}

// ScheduleMessages is the schedule's newest messages, one MessageStatus
// per (message, consumer group that receives it), newest message first.
// Messages older than the target topic's retention window are gone.
// Returns ErrScheduleNotFound if name isn't registered.
func (a *MessageAdmin) ScheduleMessages(ctx context.Context, name string, limit int) ([]*schedule.MessageStatus, error) {
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

	return a.scheduleController.ListMessages(ctx, found.TopicId, found.Name, limit)
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
