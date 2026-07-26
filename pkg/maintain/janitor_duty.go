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

// Janitor runs a topic's janitor duty:
// - create-ahead
// - retention drops/sweeps
// - idempotency-key sweep.
// Scoped to (topic) -- one effective Janitor per topic
type Janitor struct {
	Topic     *topic.Topic // resolved by Register from the name/version given to NewJanitor
	Datastore *MaintenanceDatastore
	Config    *MaintainerConfig
	Logger    logger.Logger // copied from Config.Logger at construction

	topicName      string
	version        topic.SchemaVersion
	topicDatastore *topic.TopicDatastore
	duty           *dutyRunner // constructed by Register -- topic id and rate come from the registry row
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewJanitor(topicName string, version topic.SchemaVersion, ds *datastore.PostgresDatastore, cfg *MaintainerConfig) (*Janitor, error) {
	if topicName == "" {
		return nil, errors.New("topic name is required")
	}
	if version < 1 {
		return nil, fmt.Errorf("SchemaVersion must be >= 1, got %d", version)
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

	return &Janitor{
		Datastore:      maintenanceDatastore,
		Config:         cfg,
		Logger:         cfg.Logger,
		topicName:      topicName,
		version:        version,
		topicDatastore: topicDatastore,
	}, nil
}

// Register resolves the topic by name against the live topic row.
func (j *Janitor) Register(ctx context.Context) error {
	if j.Topic != nil {
		return fmt.Errorf("janitor for topic %q already registered", j.topicName)
	}

	current, err := j.topicDatastore.GetTopic(ctx, j.topicName, j.version)
	if err != nil {
		return err
	}
	if current == nil {
		return fmt.Errorf("%w: topic %q -- register it with MessageAdmin.RegisterTopic first", topic.ErrTopicNotFound, j.topicName)
	}

	if err := migrate.AssertSchemaSupported(ctx, j.topicDatastore.Datastore.Pool, current.Id); err != nil {
		return err
	}

	duty, err := newDutyRunner(j.Datastore, j.Logger, j.Config.JitterFraction, DutyJanitor, current.Id, "", current.JanitorPollRate)
	if err != nil {
		return err
	}

	j.Topic = current
	j.duty = duty
	return nil
}

// Run ticks the janitor duty until ctx cancels; a requested stop returns nil.
func (j *Janitor) Run(ctx context.Context) error {
	if j.Topic == nil {
		return errors.New("janitor not registered -- call Register first")
	}

	j.Logger.InfoContext(ctx, "janitor duty loop starting", "topic", j.Topic.Id, "version", j.version)

	err := j.duty.run(ctx, j.sweep)
	if errors.Is(err, context.Canceled) {
		j.Logger.InfoContext(ctx, "janitor stopped", "topic", j.Topic.Id, "version", j.version)
		return nil
	}
	return err
}

// sweep is one janitor pass -- create-ahead first so retention backlogs never
// delay it; producers self-heal a missed partition but ideally shouldn't have to.
func (j *Janitor) sweep(ctx context.Context) error {
	t := j.Topic
	if err := j.Datastore.EnsureNextPartition(ctx, t.Id, t.PartitionSize); err != nil {
		return err
	}
	if err := j.Datastore.DropExpiredPartitions(ctx, t.Id, t.PartitionSize, t.RetentionTTL, t.AllowDropPastCommitted, t.DisableDeliveryLog); err != nil {
		return err
	}
	if err := j.Datastore.SweepExpiredPartitions(ctx, t.Id, t.PartitionSize, t.RetentionTTL, t.AllowDropPastCommitted, t.JanitorSweepBatchSize, t.DisableDeliveryLog); err != nil {
		return err
	}
	return j.Datastore.SweepExpiredIdempotencyKeys(ctx, t.Id, t.IdempotencyKeyTTL, t.JanitorSweepBatchSize)
}
