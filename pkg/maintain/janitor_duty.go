package maintain

import (
	"context"
	"errors"
	"fmt"

	"github.com/agentstax/vulkan/pkg/common"
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
	Topic     *topic.Topic // resolved by Register from the owner's topic id
	Datastore *MaintenanceDatastore
	Config    *MaintainerConfig
	Logger    logger.Logger // copied from Config.Logger at construction

	topicDatastore *topic.TopicDatastore
	duty           *dutyRunner // constructed by Register -- identity and tuning come from the offered maintenance row
	sweepBatchSize int         // from the offered row's metadata, like the poll rate
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewJanitor(ds *datastore.PostgresDatastore, cfg *MaintainerConfig) (*Janitor, error) {
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
		Logger:    cfg.Logger,
		Retry:     cfg.Retry,
		DutyRetry: cfg.DutyRetry,
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
		topicDatastore: topicDatastore,
	}, nil
}

// shouldRegister reports whether this duty runs the passed duty kind.
func (j *Janitor) shouldRegister(duty string) bool {
	return duty == DutyJanitor
}

// Register resolves the offered row's topic against the live topic row.
// (false, nil) declines a row of another kind.
func (j *Janitor) Register(ctx context.Context, duty string, owner *common.Owner, meta *DutyMetadata) (bool, error) {
	if !j.shouldRegister(duty) {
		return false, nil
	}
	if j.Topic != nil {
		return false, fmt.Errorf("janitor for topic %d already registered", j.Topic.Id)
	}
	if owner == nil {
		return false, errors.New("owner must not be nil")
	}
	if meta == nil {
		return false, errors.New("metadata must not be nil")
	}
	if meta.SweepBatchSize <= 0 {
		return false, fmt.Errorf("SweepBatchSize must be > 0, got %d", meta.SweepBatchSize)
	}

	current, err := j.topicDatastore.GetTopicById(ctx, owner.TopicId)
	if err != nil {
		return false, err
	}
	if current == nil {
		return false, fmt.Errorf("%w: topic %d -- register it with MessageAdmin.RegisterTopic first", topic.ErrTopicNotFound, owner.TopicId)
	}

	if err := migrate.AssertSchemaSupported(ctx, j.topicDatastore.Datastore.Pool, current.SystemId, current.Id); err != nil {
		return false, err
	}

	runner, err := newDutyRunner(j.Datastore, j.Logger, j.Config.JitterFraction, DutyJanitor, owner, meta.PollRate)
	if err != nil {
		return false, err
	}

	j.Topic = current
	j.duty = runner
	j.sweepBatchSize = meta.SweepBatchSize
	return true, nil
}

// Run ticks the janitor duty until ctx cancels; a requested stop returns nil.
func (j *Janitor) Run(ctx context.Context) error {
	if j.Topic == nil {
		return errors.New("janitor not registered -- call Register first")
	}

	j.Logger.InfoContext(ctx, "janitor duty loop starting", "topic", j.Topic.Id, "version", j.Topic.SchemaVersion)

	err := j.duty.run(ctx, j.sweep)
	if errors.Is(err, context.Canceled) {
		j.Logger.InfoContext(ctx, "janitor stopped", "topic", j.Topic.Id, "version", j.Topic.SchemaVersion)
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
	if err := j.Datastore.SweepExpiredPartitions(ctx, t.Id, t.PartitionSize, t.RetentionTTL, t.AllowDropPastCommitted, j.sweepBatchSize, t.DisableDeliveryLog); err != nil {
		return err
	}
	if err := j.Datastore.SweepExpiredIdempotencyKeys(ctx, t.Id, t.IdempotencyKeyTTL, j.sweepBatchSize); err != nil {
		return err
	}
	return j.Datastore.SweepExpiredKeyLeases(ctx, t.Id, j.sweepBatchSize)
}
