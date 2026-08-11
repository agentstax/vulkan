package datastore

import (
	"context"
	"fmt"
	"time"

	iTopic "github.com/agentstax/vulkan/internal/topic"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/jackc/pgx/v5"
)

// ddlLockTimeout caps how long partition queries wait for their table lock.
// WAITING is the hazard, not failing: postgres queues every later produce and
// claim behind the lock, so a stuck lock holder would stall the whole topic.
const ddlLockTimeout = 2 * time.Second

// DropExpiredPartitions drops each surviving partition whose newest row is
// past ttl, skipping the active partition and (unless overridden) anything a
// lagging group hasn't committed past yet -- both CURSOR and LIFECYCLE groups
// track that through cursor.committed. deliveryLogMode off skips the
// delivery_log_<topic_id> half of each drop's orphan cleanup.
func (d *JanitorDatastore) DropExpiredPartitions(ctx context.Context, topicID int64, partitionSize int64, ttl time.Duration, allowDropPastCommitted bool, deliveryLogMode topic.DeliveryLogMode) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.dropExpiredPartitions(ctx, topicID, partitionSize, ttl, allowDropPastCommitted, deliveryLogMode)
	})
}

func (d *JanitorDatastore) dropExpiredPartitions(ctx context.Context, topicID int64, partitionSize int64, ttl time.Duration, allowDropPastCommitted bool, deliveryLogMode topic.DeliveryLogMode) error {
	if ttl <= 0 {
		return nil // retention disabled - partitions kept forever
	}

	headSql := fmt.Sprintf(`
		SELECT COALESCE(MAX(id), 0) FROM %s;
	`, iTopic.MessageLogTable(topicID))
	var head int64
	if err := d.Datastore.Pool.QueryRow(ctx, headSql).Scan(&head); err != nil {
		return err
	}
	activePartition := head / partitionSize

	partitions, err := d.existingPartitions(ctx, topicID)
	if err != nil {
		return err
	}

	floor, err := d.cursorFloor(ctx, topicID)
	if err != nil {
		return err
	}

	for _, n := range partitions {
		if n >= activePartition {
			continue // never touch the active partition, or anything at/after it
		}

		expired, err := d.partitionExpired(ctx, topicID, n, ttl)
		if err != nil {
			return err
		}
		if !expired {
			continue // not this partition's turn yet -- each partition is judged independently
		}

		lastIdInPartition := (n+1)*partitionSize - 1
		if !allowDropPastCommitted && floor != nil && lastIdInPartition > *floor {
			continue // a lagging group hasn't resolved this range yet
		}

		if err := d.dropPartition(ctx, topicID, n, partitionSize, deliveryLogMode); err != nil {
			return err
		}
		d.Logger.InfoContext(ctx, "partition dropped (retention expired)", "topic_id", topicID, "partition", n)
	}

	return nil
}

// dropPartition removes the partition and its delivery/delivery_log rows in
// one transaction.
func (d *JanitorDatastore) dropPartition(ctx context.Context, topicID int64, n int64, partitionSize int64, deliveryLogMode topic.DeliveryLogMode) error {
	tx, err := d.Datastore.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// also cap the orphan DELETEs' row-lock waits
	if _, err := tx.Exec(ctx, fmt.Sprintf(`SET LOCAL lock_timeout = '%dms';`, ddlLockTimeout.Milliseconds())); err != nil {
		return err
	}

	low := n * partitionSize
	high := (n + 1) * partitionSize

	// otherwise these delivery rows (mostly 'dead' DLQ, since live ones are
	// already floor-protected) would join to nothing and park forever.
	orphanSql := fmt.Sprintf(`
		DELETE FROM %s
		WHERE message_id >= $1
			AND message_id < $2;
	`, iTopic.DeliveryTable(topicID))
	if _, err := tx.Exec(ctx, orphanSql, low, high); err != nil {
		return err
	}

	if deliveryLogMode != topic.DeliveryLogModeOff {
		orphanLogSql := fmt.Sprintf(`
			DELETE FROM %s
			WHERE message_id >= $1
				AND message_id < $2;
		`, iTopic.DeliveryLogTable(topicID))
		if _, err := tx.Exec(ctx, orphanLogSql, low, high); err != nil {
			return err
		}
	}

	// a dropped partition holding a key's latest row is a dormant key expiring
	// drop the now-dangling pointer rather than leave it forever
	orphanKeySql := `
		DELETE FROM compaction_head
		WHERE topic_id = $1
			AND head_id >= $2
			AND head_id < $3;
	`
	if _, err := tx.Exec(ctx, orphanKeySql, topicID, low, high); err != nil {
		return err
	}

	dropSql := fmt.Sprintf(`
		DROP TABLE IF EXISTS %s;
	`, iTopic.MessageLogPartitionTable(topicID, n))

	if _, err := tx.Exec(ctx, dropSql); err != nil {
		return err
	}

	return tx.Commit(ctx)
}
