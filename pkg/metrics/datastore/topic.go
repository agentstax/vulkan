package datastore

import "context"

// IsCompacted reports whether topicID has ever seen a keyed publish -- any
// compaction_head row means latest-per-key winners outlive retention.
func (d *MetricsDatastore) IsCompacted(ctx context.Context, topicID int64) (bool, error) {
	var compacted bool
	err := d.Retry.Wrap(ctx, func() error {
		var err error
		compacted, err = d.isCompacted(ctx, topicID)
		return err
	})
	return compacted, err
}

func (d *MetricsDatastore) isCompacted(ctx context.Context, topicID int64) (bool, error) {
	var compacted bool
	err := d.Datastore.Pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM compaction_head WHERE topic_id = $1);`, topicID,
	).Scan(&compacted)
	return compacted, err
}
