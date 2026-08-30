package scheduler

import (
	"context"
	"errors"

	"github.com/agentstax/vulkan/pkg/common/logging"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/migrate"
	"github.com/agentstax/vulkan/pkg/schedule"
	schedulecontroller "github.com/agentstax/vulkan/pkg/schedule/controller"
	systemcontroller "github.com/agentstax/vulkan/pkg/system/controller"
	"github.com/agentstax/vulkan/pkg/systemmanager"
	"github.com/agentstax/vulkan/pkg/topic"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
)

// Scheduler declares schedules; the system's schedule producer worker is what produces them.
type Scheduler struct {
	Config *SchedulerConfig
	Logger logging.Logger

	ds *datastore.PostgresDatastore

	systemController   *systemcontroller.SystemController
	topicController    *topiccontroller.TopicController
	scheduleController *schedulecontroller.ScheduleController
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewScheduler(ds *datastore.PostgresDatastore, cfg *SchedulerConfig) (*Scheduler, error) {
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}
	if cfg == nil {
		cfg = &SchedulerConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	cfg.Logger = logging.NewPipelineLogger(cfg.Logger, &logging.PipelineLoggerConfig{Buffer: true, Suppress: true})

	systemController, err := systemcontroller.NewSystemController(ds, &systemcontroller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	topicController, err := topiccontroller.NewTopicController(ds, &topiccontroller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	scheduleController, err := schedulecontroller.NewScheduleController(ds, &schedulecontroller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &Scheduler{
		Config:             cfg,
		Logger:             cfg.Logger,
		ds:                 ds,
		systemController:   systemController,
		topicController:    topicController,
		scheduleController: scheduleController,
	}, nil
}

// Register declares the schedule named name on the target topic and returns
// an instance for it. Safe to call on every startup: the newest declaration
// wins, so two services passing different values for one name overwrite
// each other. A changed expression drops a time already due under the old
// one; a suspended schedule stays suspended. The name is the message key of
// every produce. cfg may be nil or sparse.
// ctx bounds only this call's I/O; the instance's lifetime is Schedule's ctx.
func (s *Scheduler) Register[Message topic.Versioned](ctx context.Context, name string, expression string, topicName string, payload *Message, cfg *schedulecontroller.ScheduleConfig) (*SchedulerInstance[Message], error) {
	if name == "" {
		return nil, errors.New("schedule name is required")
	}
	if expression == "" {
		return nil, errors.New("expression is required")
	}
	if topicName == "" {
		return nil, errors.New("topic name is required")
	}
	parsed, err := schedule.ParseExpression(expression)
	if err != nil {
		return nil, err
	}

	// gate -- a schedule needs the control-plane schema RegisterSystem creates
	sys, err := s.systemController.Get(ctx)
	if err != nil {
		return nil, err
	}
	if sys == nil {
		return nil, migrate.ErrNotRegistered.With("schedule", name)
	}

	target, err := s.topicController.Get(ctx, topicName)
	if err != nil {
		return nil, err
	}
	if target == nil {
		return nil, topic.ErrTopicNotFound.With("topic", topicName)
	}
	if err := s.topicController.AssertSchemaSupported(ctx, target.SystemId, target.Id); err != nil {
		return nil, err
	}

	if target.DeliveryLogMode != topic.DeliveryLogModeAll {
		s.Logger.WarnContext(ctx, schedule.EventTargetKeepsNoSuccessRows.Message, "code", schedule.EventTargetKeepsNoSuccessRows.Code, "schedule", name, "topic", target.Name, "delivery_log_mode", string(target.DeliveryLogMode))
	}

	registered, err := s.scheduleController.Register(ctx, sys.Id, name, parsed, target.Id, payload, cfg)
	if err != nil {
		return nil, err
	}

	// built per instance -- a SystemManager refuses a second concurrent Run
	systemManager, err := systemmanager.NewSystemManager(s.ds, &systemmanager.SystemManagerConfig{
		Logger: s.Config.Logger,
		Retry:  s.Config.Retry,
	})
	if err != nil {
		return nil, err
	}

	return newSchedulerInstance[Message](registered, payload, systemManager, s.Config)
}
