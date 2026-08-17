package datastore

import "context"

// SweepExpiredKeyLeases deletes this topic's expired key_lease rows.
// A crashed consumer leaves its expired row behind:
//   - the key gets another message -> that claim takes the row over
//   - the key never does -> the row sits forever, only this sweep removes it
func (d *JanitorDatastore) SweepExpiredKeyLeases(ctx context.Context, topicId int64, batchSize int) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.sweepExpiredKeyLeases(ctx, topicId, batchSize)
	})
}

func (d *JanitorDatastore) sweepExpiredKeyLeases(ctx context.Context, topicId int64, batchSize int) error {
	// protect against any potential infinite loops
	const maxKeyLeaseSweepBatches = 1000
	for range maxKeyLeaseSweepBatches {
		swept, err := d.sweepKeyLeasesBatch(ctx, topicId, batchSize)
		if err != nil {
			return err
		}
		if swept < batchSize {
			break // ran out of expired rows
		}
	}
	return nil
}

// racing a consumer acquiring the same key is safe: if the delete wins, the
// consumer's upsert inserts a fresh row instead of updating the expired one
func (d *JanitorDatastore) sweepKeyLeasesBatch(ctx context.Context, topicId int64, batchSize int) (int, error) {
	sql := `
		DELETE FROM key_lease
		WHERE (consumer_group_id, compaction_key) IN (
			SELECT k.consumer_group_id, k.compaction_key
			FROM key_lease k
			JOIN consumer_group g ON g.id = k.consumer_group_id
			WHERE g.topic_id = $1
				AND k.expires_at < now()
			LIMIT $2
		);
	`

	tag, err := d.Datastore.Pool.Exec(ctx, sql, topicId, batchSize)
	if err != nil {
		return 0, err
	}

	return int(tag.RowsAffected()), nil
}
