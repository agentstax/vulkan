package scheduler

import (
	"context"
	"errors"

	"github.com/agentstax/vulkan/pkg/common/logging"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/schedule"
	"github.com/agentstax/vulkan/pkg/systemmanager"
	"github.com/agentstax/vulkan/pkg/topic"
)

// SchedulerInstance is a registered schedule: Schedule keeps the system producing it.
type SchedulerInstance[Message topic.Versioned] struct {
	Registered *schedule.ScheduleData
	Payload    *Message
	Config     *SchedulerConfig
	Logger     logging.Logger

	ds *datastore.PostgresDatastore
}

// cfg arrives already resolved by NewScheduler -- Register is the only caller.
func newSchedulerInstance[Message topic.Versioned](registered *schedule.ScheduleData, payload *Message, ds *datastore.PostgresDatastore, cfg *SchedulerConfig) (*SchedulerInstance[Message], error) {
	if registered == nil {
		return nil, errors.New("schedule must not be nil")
	}
	if payload == nil {
		return nil, errors.New("payload must not be nil")
	}
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}
	if cfg == nil {
		return nil, errors.New("config must not be nil")
	}
	return &SchedulerInstance[Message]{
		Registered: registered,
		Payload:    payload,
		Config:     cfg,
		Logger:     cfg.Logger,
		ds:         ds,
	}, nil
}

// Schedule runs the system manager until ctx cancels -- what `vulkan manager
// run` does; the schedule producer worker produces every registered
// schedule, not just this one. A requested stop returns nil.
func (i *SchedulerInstance[Message]) Schedule(ctx context.Context) error {
	// built per call -- a SystemManager refuses a second concurrent Run
	systemManager, err := systemmanager.NewSystemManager(i.ds, &systemmanager.SystemManagerConfig{
		Logger: i.Config.Logger,
		Retry:  i.Config.Retry,
	})
	if err != nil {
		return err
	}
	return systemManager.Run(ctx)
}
