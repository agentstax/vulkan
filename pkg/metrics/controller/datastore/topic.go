package datastore

import "context"

// IsCompacted reports whether topicId has ever seen a keyed publish -- any
// compaction_head row means latest-per-key winners outlive retention.
func (d *MetricsDatastore) IsCompacted(ctx context.Context, topicId int64) (bool, error) {
	var compacted bool
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		compacted, err = d.isCompacted(ctx, topicId)
		return err
	})
	return compacted, err
}

func (d *MetricsDatastore) isCompacted(ctx context.Context, topicId int64) (bool, error) {
	var compacted bool
	err := d.Datastore.Pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM compaction_head WHERE topic_id = $1);`, topicId,
	).Scan(&compacted)
	return compacted, err
}
