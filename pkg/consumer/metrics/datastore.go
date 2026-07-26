package metrics

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/internal/topic"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/retry"
)

// ConsumerMetricsDatastore is the read-only view of a group's live DB truth.
// Exported (unlike the maintain metrics datastore) because pkg/metrics
// composes health verdicts from its snapshots.
type ConsumerMetricsDatastore struct {
	Datastore *datastore.PostgresDatastore
	Retry     *retry.DatastoreRetry
	Logger    logger.Logger
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewConsumerMetricsDatastore(ds *datastore.PostgresDatastore, cfg *ConsumerMetricsDatastoreConfig) (*ConsumerMetricsDatastore, error) {
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}
	if cfg == nil {
		cfg = &ConsumerMetricsDatastoreConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	dsRetry, err := retry.NewDatastoreRetry(cfg.Retry, cfg.Logger)
	if err != nil {
		return nil, err
	}

	return &ConsumerMetricsDatastore{
		Datastore: ds,
		Retry:     dsRetry,
		Logger:    cfg.Logger,
	}, nil
}

// ConsumerGroupSnapshot is the current picture for (topicID, consumerGroup),
// queried live from Postgres -- works cold, nothing needs to be running.
func (d *ConsumerMetricsDatastore) ConsumerGroupSnapshot(ctx context.Context, topicID int64, consumerGroup string) (*ConsumerGroupSnapshot, error) {
	var snapshot *ConsumerGroupSnapshot
	err := d.Retry.Wrap(ctx, func() error {
		var err error
		snapshot, err = d.consumerGroupSnapshot(ctx, topicID, consumerGroup)
		return err
	})
	return snapshot, err
}

func (d *ConsumerMetricsDatastore) consumerGroupSnapshot(ctx context.Context, topicID int64, consumerGroup string) (*ConsumerGroupSnapshot, error) {
	sql := fmt.Sprintf(`
		SELECT
			c.claimed,
			c.committed,
			COALESCE((
				SELECT MAX(id)
				FROM %[1]s
			), 0) AS head,
			COALESCE((
				SELECT COUNT(*)
				FROM %[2]s
				WHERE consumer_group = $1 AND status = 'ready'
			), 0) AS ready_exceptions,
			COALESCE((
				SELECT COUNT(*)
				FROM %[2]s
				WHERE consumer_group = $1 AND status = 'inflight'
			), 0) AS inflight_exceptions,
			COALESCE((
				SELECT COUNT(*)
				FROM %[2]s
				WHERE consumer_group = $1 AND status = 'dead'
			), 0) AS dead_exceptions,
			(
				SELECT MIN(created_at)
				FROM %[2]s
				WHERE consumer_group = $1 AND status IN ('ready', 'inflight')
			) AS oldest_unacked_at,
			COALESCE((
				SELECT COUNT(*)
				FROM lease
				WHERE consumer_group = $1 AND topic_id = $2
			), 0) AS open_leases
		FROM cursor c
		WHERE c.consumer_group = $1 AND c.topic_id = $2;
	`, topic.MessageLogTable(topicID), topic.DeliveryTable(topicID))

	var s ConsumerGroupSnapshot
	var oldestUnackedAt *time.Time
	err := d.Datastore.Pool.QueryRow(ctx, sql, consumerGroup, topicID).Scan(
		&s.Claimed,
		&s.Committed,
		&s.Head,
		&s.ReadyExceptions,
		&s.InflightExceptions,
		&s.DeadExceptions,
		&oldestUnackedAt,
		&s.OpenLeases,
	)
	if err != nil {
		return nil, err
	}

	s.ConsumerGroup = consumerGroup
	s.Backlog = s.Head - s.Committed
	s.Inflight = s.Claimed - s.Committed
	if oldestUnackedAt != nil {
		s.OldestUnackedAge = time.Since(*oldestUnackedAt)
	}

	return &s, nil
}
