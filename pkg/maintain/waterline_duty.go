package maintain

import (
	"context"
	"errors"
	"fmt"

	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/migrate"
	"github.com/agentstax/vulkan/pkg/topic"
)

// WaterlineRoller runs one consumer group's waterline duty:
// - periodically rolling cursor.committed up behind the group's resolved work.
// Scoped to (topic, group) -- one effective roller per cursor.
type WaterlineRoller struct {
	Topic     *topic.Topic // resolved by Register from the name given to NewWaterlineRoller
	Datastore *MaintenanceDatastore
	Config    *MaintainerConfig
	Logger    logger.Logger // copied from Config.Logger at construction

	consumerGroup  string
	topicName      string
	topicDatastore *topic.TopicDatastore
	duty           *dutyRunner // constructed by Register -- topic id and rate come from the registry row
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewWaterlineRoller(consumerGroup string, topicName string, ds *datastore.PostgresDatastore, cfg *MaintainerConfig) (*WaterlineRoller, error) {
	if consumerGroup == "" {
		return nil, errors.New("consumer group is required")
	}
	if topicName == "" {
		return nil, errors.New("topic name is required")
	}
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}

	if cfg == nil {
		cfg = &MaintainerConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	maintenanceDatastore, err := NewMaintenanceDatastore(ds, &MaintenanceDatastoreConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	topicDatastore, err := topic.NewTopicDatastore(ds, cfg.Retry, cfg.Logger)
	if err != nil {
		return nil, err
	}

	return &WaterlineRoller{
		Datastore:      maintenanceDatastore,
		Config:         cfg,
		Logger:         cfg.Logger,
		consumerGroup:  consumerGroup,
		topicName:      topicName,
		topicDatastore: topicDatastore,
	}, nil
}

// Register resolves the topic by name against the live topic row.
func (w *WaterlineRoller) Register(ctx context.Context) error {
	if w.Topic != nil {
		return fmt.Errorf("waterline roller for group %q on topic %q already registered", w.consumerGroup, w.topicName)
	}

	current, err := w.topicDatastore.GetTopic(ctx, w.topicName)
	if err != nil {
		return err
	}
	if current == nil {
		return fmt.Errorf("%w: topic %q -- register it with MessageAdmin.RegisterTopic first", topic.ErrTopicNotFound, w.topicName)
	}

	if err := migrate.AssertSchemaSupported(ctx, w.topicDatastore.Datastore.Pool, current.Id); err != nil {
		return err
	}

	duty, err := newDutyRunner(w.Datastore, w.Logger, w.Config.JitterFraction, DutyWaterline, current.Id, w.consumerGroup, current.WaterlinePollRate)
	if err != nil {
		return err
	}

	w.Topic = current
	w.duty = duty
	return nil
}

// Run ticks the waterline duty until ctx cancels; a requested stop returns nil.
func (w *WaterlineRoller) Run(ctx context.Context) error {
	if w.Topic == nil {
		return errors.New("waterline roller not registered -- call Register first")
	}

	w.Logger.InfoContext(ctx, "waterline duty loop starting", "topic", w.Topic.Id, "group", w.consumerGroup)

	err := w.duty.run(ctx, w.roll)
	if errors.Is(err, context.Canceled) {
		w.Logger.InfoContext(ctx, "waterline roller stopped", "topic", w.Topic.Id, "group", w.consumerGroup)
		return nil
	}
	return err
}

func (w *WaterlineRoller) roll(ctx context.Context) error {
	_, err := w.Datastore.AdvanceWaterline(ctx, w.Topic.Id, w.consumerGroup)
	return err
}
