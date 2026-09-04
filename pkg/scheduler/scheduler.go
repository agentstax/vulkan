package scheduler

import (
	"context"
	"errors"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/common/logging"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/migrate"
	"github.com/agentstax/vulkan/pkg/schedule"
	schedulecontroller "github.com/agentstax/vulkan/pkg/schedule/controller"
	systemcontroller "github.com/agentstax/vulkan/pkg/system/controller"
	"github.com/agentstax/vulkan/pkg/topic"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
)

// Scheduler declares schedules; the system's schedule producer worker is what produces them.
type Scheduler struct {
	ds *datastore.PostgresDatastore
}

// NewScheduler builds the datastore-only registration object. Register owns
// the config because each call returns an independently configured instance.
func NewScheduler(ds *datastore.PostgresDatastore) (*Scheduler, error) {
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}
	return &Scheduler{ds: ds}, nil
}

// Register declares the named schedule on its target topic and returns an
// instance for it. Safe to call on every startup: the newest registration
// wins, so two services passing different values for one name overwrite
// each other. A changed expression drops a time already due under the old
// one; a suspended schedule stays suspended. The name is the message key of
// every produce. cfg may be nil or sparse.
// ctx bounds only this call's I/O; the instance's lifetime is Schedule's ctx.
func (s *Scheduler) Register[Message common.Versioned](ctx context.Context, name string, topicName string, cron string, payload *Message, cfg *SchedulerConfig) (*SchedulerInstance[Message], error) {
	if name == "" {
		return nil, errors.New("schedule name is required")
	}
	if topicName == "" {
		return nil, errors.New("topic name is required")
	}
	if cfg == nil {
		cfg = &SchedulerConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	cfg.Logger = logging.NewPipelineLogger(cfg.Logger, &logging.PipelineLoggerConfig{Buffer: true, Suppress: true})

	systemController, err := systemcontroller.NewSystemController(s.ds, &systemcontroller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}
	topicController, err := topiccontroller.NewTopicController(s.ds, &topiccontroller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}
	scheduleController, err := schedulecontroller.NewScheduleController(s.ds, &schedulecontroller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	// gate -- a schedule needs the control-plane tables RegisterSystem creates
	sys, err := systemController.Get(ctx)
	if err != nil {
		return nil, err
	}
	if sys == nil {
		return nil, migrate.ErrNotRegistered.With("schedule", name)
	}

	target, err := topicController.Get(ctx, topicName)
	if err != nil {
		return nil, err
	}
	if target == nil {
		return nil, topic.ErrTopicNotFound.With("topic", topicName)
	}
	if err := topicController.AssertSchemaSupported(ctx, target.SystemId, target.Id); err != nil {
		return nil, err
	}

	if target.DeliveryLogMode != topic.DeliveryLogModeAll {
		cfg.Logger.WarnContext(ctx, schedule.EventTargetKeepsNoSuccessRows.Message, "code", schedule.EventTargetKeepsNoSuccessRows.Code, "schedule", name, "topic", target.Name, "delivery_log_mode", string(target.DeliveryLogMode))
	}

	registered, err := scheduleController.Register(ctx, sys.Id, name, cron, target.Id, payload, cfg.Timeout, cfg.Concurrency, cfg.Metadata)
	if err != nil {
		return nil, err
	}

	return newSchedulerInstance[Message](registered, payload, s.ds, cfg)
}
