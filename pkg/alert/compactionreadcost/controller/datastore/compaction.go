package datastore

import (
	"context"
	"fmt"

	"github.com/agentstax/vulkan/pkg/topic"
)

// IsCompacted reports whether the topic has any compaction_head rows.
func (d *CompactionReadCostDatastore) IsCompacted(ctx context.Context, topicId int64) (bool, error) {
	var compacted bool
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		compacted, err = d.isCompacted(ctx, topicId)
		return err
	})
	return compacted, err
}

func (d *CompactionReadCostDatastore) isCompacted(ctx context.Context, topicId int64) (bool, error) {
	sql := fmt.Sprintf(`
		-- vulkan: compactionreadcost.isCompacted
		SELECT EXISTS (SELECT 1 FROM %s);
	`, topic.CompactionHeadTable(topicId))
	var compacted bool
	err := d.Datastore.Pool.QueryRow(ctx, sql).Scan(&compacted)
	return compacted, err
}
