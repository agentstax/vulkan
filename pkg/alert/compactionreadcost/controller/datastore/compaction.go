package datastore

import "context"

// Compacted reports whether the topic has any compaction_head rows.
func (d *CompactionReadCostDatastore) Compacted(ctx context.Context, topicId int64) (bool, error) {
	var compacted bool
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		compacted, err = d.compacted(ctx, topicId)
		return err
	})
	return compacted, err
}

func (d *CompactionReadCostDatastore) compacted(ctx context.Context, topicId int64) (bool, error) {
	sql := `SELECT EXISTS (SELECT 1 FROM compaction_head WHERE topic_id = $1);`
	var compacted bool
	err := d.Datastore.Pool.QueryRow(ctx, sql, topicId).Scan(&compacted)
	return compacted, err
}
