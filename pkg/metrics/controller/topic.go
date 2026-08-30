package controller

import (
	"context"
	"fmt"

	"github.com/agentstax/vulkan/pkg/metrics"
)

// IsCompacted reports whether topicId has ever seen a keyed publish -- any
// compaction_head row means latest-per-key winners outlive retention.
func (c *MetricsController) IsCompacted(ctx context.Context, topicId int64) (bool, error) {
	if topicId <= 0 {
		return false, fmt.Errorf("topicId must be > 0, got %d", topicId)
	}
	return c.datastore.IsCompacted(ctx, topicId)
}

func (c *MetricsController) TopicSnapshot(ctx context.Context, topicId int64) (*metrics.TopicSnapshot, error) {
	compacted, err := c.IsCompacted(ctx, topicId)
	if err != nil {
		return nil, err
	}

	groupNames, err := c.ListConsumerGroups(ctx, topicId)
	if err != nil {
		return nil, err
	}

	groups := make([]metrics.ConsumerGroupSnapshot, 0, len(groupNames))
	for _, name := range groupNames {
		group, err := c.ConsumerGroupSnapshot(ctx, topicId, name)
		if err != nil {
			return nil, err
		}
		groups = append(groups, *group)
	}

	return &metrics.TopicSnapshot{TopicId: topicId, Compacted: compacted, Groups: groups}, nil
}

// SchemaVersionSnapshots is every payload version present in the topic's
// log, each with every group's lag against it.
func (c *MetricsController) SchemaVersionSnapshots(ctx context.Context, topicId int64) ([]metrics.SchemaVersionSnapshot, error) {
	if topicId <= 0 {
		return nil, fmt.Errorf("topicId must be > 0, got %d", topicId)
	}

	counts, err := c.datastore.SchemaVersionCounts(ctx, topicId)
	if err != nil {
		return nil, err
	}

	snapshots := make([]metrics.SchemaVersionSnapshot, 0, len(counts))
	for _, count := range counts {
		lags, err := c.datastore.GroupSchemaVersionLag(ctx, topicId, count.SchemaVersion)
		if err != nil {
			return nil, err
		}
		snapshots = append(snapshots, toSchemaVersionSnapshot(&count, lags))
	}
	return snapshots, nil
}
