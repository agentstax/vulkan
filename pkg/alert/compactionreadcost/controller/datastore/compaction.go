package datastore

import "context"

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
	sql := `-- vulkan: compactionreadcost.isCompacted
SELECT EXISTS (SELECT 1 FROM compaction_head WHERE topic_id = $1);`
	var compacted bool
	err := d.Datastore.Pool.QueryRow(ctx, sql, topicId).Scan(&compacted)
	return compacted, err
}
