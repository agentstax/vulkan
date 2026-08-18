package datastore

import (
	"context"
	"fmt"
	"slices"
	"time"

	iTopic "github.com/agentstax/vulkan/internal/topic"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/jackc/pgx/v5"
)

// SweepExpiredPartitions drains the ttl-expired prefix of every surviving
// partition -- covers the low-volume tail that never fills a partition wide
// enough to earn a whole-partition drop.
func (d *JanitorDatastore) SweepExpiredPartitions(ctx context.Context, topicId int64, partitionSize int64, ttl time.Duration, allowDropPastCommitted bool, batchSize int, deliveryLogMode topic.DeliveryLogMode) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.sweepExpiredPartitions(ctx, topicId, partitionSize, ttl, allowDropPastCommitted, batchSize, deliveryLogMode)
	})
}

func (d *JanitorDatastore) sweepExpiredPartitions(ctx context.Context, topicId int64, partitionSize int64, ttl time.Duration, allowDropPastCommitted bool, batchSize int, deliveryLogMode topic.DeliveryLogMode) error {
	if ttl <= 0 {
		return nil // retention disabled
	}

	partitions, err := d.existingPartitions(ctx, topicId)
	if err != nil {
		return err
	}

	cutoff := time.Now().Add(-ttl)

	// caps a full drain to break any potential infinite loops
	maxBatches := int((partitionSize + int64(batchSize) - 1) / int64(batchSize))

	for _, n := range partitions { // every partition, independently -- one backlog can't block the rest
		for range maxBatches {
			swept, err := d.sweepBatch(ctx, topicId, n, cutoff, allowDropPastCommitted, batchSize, deliveryLogMode)
			if err != nil {
				return err
			}
			if swept < batchSize {
				break // ran out of expired rows (or hit the floor)
			}
		}
	}

	return nil
}

// sweepBatch deletes up to batchSize expired rows from the front of partition n,
// plus their orphaned delivery/delivery_log rows, in one transaction.
func (d *JanitorDatastore) sweepBatch(ctx context.Context, topicId int64, n int64, cutoff time.Time, allowDropPastCommitted bool, batchSize int, deliveryLogMode topic.DeliveryLogMode) (int, error) {
	tx, err := d.Datastore.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	var floor *int64
	if !allowDropPastCommitted {
		floor, err = d.cursorFloor(ctx, tx, topicId)
		if err != nil {
			return 0, err
		}
	}

	sweepSql := fmt.Sprintf(`
		DELETE FROM %s
		WHERE id IN (
			SELECT id FROM %s
			WHERE created_at < $1
				AND ($3::bigint IS NULL OR id <= $3) -- nil floor (allowDropPastCommitted) skips the check
			ORDER BY id ASC -- walk the expired prefix from the front, same PK-index ride as partitionExpired
			LIMIT $2
		)
		RETURNING id, compaction_key;
	`, iTopic.MessageLogPartitionTable(topicId, n), iTopic.MessageLogPartitionTable(topicId, n))

	rows, err := tx.Query(ctx, sweepSql, cutoff, batchSize, floor)
	if err != nil {
		return 0, err
	}
	swept, err := pgx.CollectRows(rows, pgx.RowToStructByName[sweptRow])
	if err != nil {
		return 0, err
	}

	ids := make([]int64, len(swept))
	for i, r := range swept {
		ids[i] = r.Id
	}

	if len(ids) > 0 {
		// otherwise these delivery rows (mostly 'dead' DLQ) would join to nothing and sit there forever.
		orphanSql := fmt.Sprintf(`
			DELETE FROM %s
			WHERE message_id = ANY($1);
		`, iTopic.DeliveryTable(topicId))
		if _, err := tx.Exec(ctx, orphanSql, ids); err != nil {
			return 0, err
		}

		if deliveryLogMode != topic.DeliveryLogModeOff {
			orphanLogSql := fmt.Sprintf(`
				DELETE FROM %s
				WHERE message_id = ANY($1);
			`, iTopic.DeliveryLogTable(topicId))
			if _, err := tx.Exec(ctx, orphanLogSql, ids); err != nil {
				return 0, err
			}
		}
	}

	// most topics never use compaction at all, so most sweeps would
	// otherwise pay a delete that can never match anything
	anyKeyed := slices.ContainsFunc(swept, func(r sweptRow) bool { return r.CompactionKey != nil })

	if anyKeyed {
		orphanKeySql := `
			DELETE FROM compaction_head
			WHERE topic_id = $1
				AND head_id = ANY($2);
		`
		if _, err := tx.Exec(ctx, orphanKeySql, topicId, ids); err != nil {
			return 0, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}

	if len(ids) > 0 {
		d.Logger.DebugContext(ctx, "swept expired rows", "topic_id", topicId, "partition", n, "swept", len(ids), "batch_size", batchSize)
	}

	return len(ids), nil
}
