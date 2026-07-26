package metrics

import (
	"context"
	"errors"

	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/retry"
)

// TopicMetricsDatastore is the read-only view of a topic's live DB truth --
// bound groups, compaction presence. Exported (unlike the maintain metrics
// datastore) because pkg/metrics composes health verdicts from it.
type TopicMetricsDatastore struct {
	Datastore *datastore.PostgresDatastore
	Retry     *retry.DatastoreRetry
	Logger    logger.Logger
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewTopicMetricsDatastore(ds *datastore.PostgresDatastore, cfg *TopicMetricsDatastoreConfig) (*TopicMetricsDatastore, error) {
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}
	if cfg == nil {
		cfg = &TopicMetricsDatastoreConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	dsRetry, err := retry.NewDatastoreRetry(cfg.Retry, cfg.Logger)
	if err != nil {
		return nil, err
	}

	return &TopicMetricsDatastore{
		Datastore: ds,
		Retry:     dsRetry,
		Logger:    cfg.Logger,
	}, nil
}

// ListConsumerGroups is every group with a cursor on topicID -- the groups a
// health view must account for before the topic can be considered drained.
func (d *TopicMetricsDatastore) ListConsumerGroups(ctx context.Context, topicID int64) ([]string, error) {
	var groups []string
	err := d.Retry.Wrap(ctx, func() error {
		var err error
		groups, err = d.listConsumerGroups(ctx, topicID)
		return err
	})
	return groups, err
}

func (d *TopicMetricsDatastore) listConsumerGroups(ctx context.Context, topicID int64) ([]string, error) {
	rows, err := d.Datastore.Pool.Query(ctx,
		`SELECT consumer_group FROM cursor WHERE topic_id = $1 ORDER BY consumer_group;`, topicID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []string
	for rows.Next() {
		var g string
		if err := rows.Scan(&g); err != nil {
			return nil, err
		}
		groups = append(groups, g)
	}
	return groups, rows.Err()
}

// IsCompacted reports whether topicID has ever seen a keyed publish -- any
// compaction_head row means latest-per-key winners outlive retention.
func (d *TopicMetricsDatastore) IsCompacted(ctx context.Context, topicID int64) (bool, error) {
	var compacted bool
	err := d.Retry.Wrap(ctx, func() error {
		var err error
		compacted, err = d.isCompacted(ctx, topicID)
		return err
	})
	return compacted, err
}

func (d *TopicMetricsDatastore) isCompacted(ctx context.Context, topicID int64) (bool, error) {
	var compacted bool
	err := d.Datastore.Pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM compaction_head WHERE topic_id = $1);`, topicID,
	).Scan(&compacted)
	return compacted, err
}
