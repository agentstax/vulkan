package datastore

import (
	"context"
	"time"
)

// SweepExpiredWaitingDeclarations deletes waiting binding_log rows whose
// attempt ran more than ttl ago, at most batchSize per call, and returns how
// many were deleted.
func (d *JanitorDatastore) SweepExpiredWaitingDeclarations(ctx context.Context, ttl time.Duration, batchSize int) (int64, error) {
	var swept int64
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		swept, err = d.sweepExpiredWaitingDeclarations(ctx, ttl, batchSize)
		return err
	})
	return swept, err
}

func (d *JanitorDatastore) sweepExpiredWaitingDeclarations(ctx context.Context, ttl time.Duration, batchSize int) (int64, error) {
	cutoff := time.Now().Add(-ttl)

	// a declarer's newest waiting id is protected even past the cutoff, so
	// a dead waiter stays visible in listings. Installed rows are never
	// touched.
	sql := `
		-- vulkan: consumergroupjanitor.sweepExpiredWaitingDeclarations
		WITH newest_waiting AS (
			SELECT consumer_group_id, declared_by, max(id) AS newest_id
			FROM binding_log
			WHERE status = 'waiting'
			GROUP BY consumer_group_id, declared_by
		)
		DELETE FROM binding_log
		WHERE id IN (
			SELECT binding_log.id
			FROM binding_log
			JOIN newest_waiting ON newest_waiting.consumer_group_id = binding_log.consumer_group_id
				AND newest_waiting.declared_by = binding_log.declared_by
			WHERE binding_log.status = 'waiting'
			AND binding_log.attempt_at < $1
			AND binding_log.id < newest_waiting.newest_id
			LIMIT $2
		);
	`
	tag, err := d.Datastore.Pool.Exec(ctx, sql, cutoff, batchSize)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
