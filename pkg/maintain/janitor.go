package maintain

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/agentstax/vulkan/internal/topic"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ddlLockTimeout caps how long partition queries waits for its table lock.
// WAITING is the hazard, not failing: postgres queues every later produce and
// claim behind the lock, so a stuck lock holder would stall the whole topic.
const ddlLockTimeout = 2 * time.Second

// The janitor duty's four ops, in tick order:
// - create-ahead
// - whole-partition drop
// - expired-prefix sweep
// - idempotency-key sweep.
// All topicID-scoped; each is idempotent and concurrent-safe.
func (d *MaintenanceDatastore) EnsureNextPartition(ctx context.Context, topicID int64, partitionSize int64) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.ensureNextPartition(ctx, topicID, partitionSize)
	})
}

// ensureNextPartition keeps the partition after head's created at all times.
// An empty partition ahead is free (no storage, no locks on the no-op CREATE,
// invisible to retention); a missed boundary fails in-flight produces into
// the self-heal path.
func (d *MaintenanceDatastore) ensureNextPartition(ctx context.Context, topicID int64, partitionSize int64) error {
	headSql := fmt.Sprintf(`
		SELECT COALESCE(MAX(id), 0) FROM %s;
	`, topic.MessageLogTable(topicID))

	var head int64
	if err := d.Datastore.Pool.QueryRow(ctx, headSql).Scan(&head); err != nil {
		return err
	}

	nextPartition := head/partitionSize + 1

	createPartitionSql := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s
			PARTITION OF %s
			FOR VALUES FROM (%d) TO (%d);
	`, topic.MessageLogPartitionTable(topicID, nextPartition), topic.MessageLogTable(topicID), nextPartition*partitionSize, (nextPartition+1)*partitionSize)

	// one round trip -- a batch outside an explicit txn runs as one implicit
	// transaction, which scopes the SET LOCAL to exactly these two statements
	// instead of leaking it to whatever might use this pooled connection next
	batch := &pgx.Batch{}
	batch.Queue(fmt.Sprintf(`SET LOCAL lock_timeout = '%dms';`, ddlLockTimeout.Milliseconds()))
	batch.Queue(createPartitionSql)

	results := d.Datastore.Pool.SendBatch(ctx, batch)
	if _, err := results.Exec(); err != nil {
		results.Close()
		return err
	}
	_, err := results.Exec()
	closeErr := results.Close()
	if err != nil {
		// IF NOT EXISTS still races -- losing to a concurrent creator means it exists
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "42P07" {
			return nil
		}
		return err
	}
	if closeErr != nil {
		return closeErr
	}

	d.Logger.InfoContext(ctx, "partition created", "topic_id", topicID, "partition", nextPartition)
	return nil
}

// DropExpiredPartitions drops each surviving partition whose newest row is
// past ttl, skipping the active partition and (unless overridden) anything a
// lagging group hasn't committed past yet -- both CURSOR and LIFECYCLE groups
// track that through cursor.committed. disableDeliveryLog skips the
// delivery_log_<topic_id> half of each drop's orphan cleanup.
func (d *MaintenanceDatastore) DropExpiredPartitions(ctx context.Context, topicID int64, partitionSize int64, ttl time.Duration, allowDropPastCommitted bool, disableDeliveryLog bool) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.dropExpiredPartitions(ctx, topicID, partitionSize, ttl, allowDropPastCommitted, disableDeliveryLog)
	})
}

func (d *MaintenanceDatastore) dropExpiredPartitions(ctx context.Context, topicID int64, partitionSize int64, ttl time.Duration, allowDropPastCommitted bool, disableDeliveryLog bool) error {
	if ttl <= 0 {
		return nil // retention disabled - partitions kept forever
	}

	headSql := fmt.Sprintf(`
		SELECT COALESCE(MAX(id), 0) FROM %s;
	`, topic.MessageLogTable(topicID))
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

		if err := d.dropPartition(ctx, topicID, n, partitionSize, disableDeliveryLog); err != nil {
			return err
		}
		d.Logger.InfoContext(ctx, "partition dropped (retention expired)", "topic_id", topicID, "partition", n)
	}

	return nil
}

// existingPartitions lists surviving message_log_<topic_id>_<n> partition numbers.
func (d *MaintenanceDatastore) existingPartitions(ctx context.Context, topicID int64) ([]int64, error) {
	sql := fmt.Sprintf(`
		SELECT REPLACE(c.relname, '%s_', '')::bigint AS n
		FROM pg_inherits i
		JOIN pg_class c ON c.oid = i.inhrelid
		WHERE i.inhparent = '%s'::regclass;
	`, topic.MessageLogTable(topicID), topic.MessageLogTable(topicID))

	rows, err := d.Datastore.Pool.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var partitions []int64
	for rows.Next() {
		var n int64
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}

		partitions = append(partitions, n)
	}

	return partitions, rows.Err()
}

// cursorFloor is the waterline floor: the most-lagging group's committed
// offset within this topic (nil if none exist yet). Scoped through the group
// registry so a lagging group on another topic can't block this topic's
// drops/sweeps.
func (d *MaintenanceDatastore) cursorFloor(ctx context.Context, topicID int64) (*int64, error) {
	sql := `
		SELECT MIN(c.committed)
		FROM cursor c
		JOIN consumer_group g ON g.id = c.consumer_group_id
		WHERE g.topic_id = $1;
	`

	var floor *int64
	err := d.Datastore.Pool.QueryRow(ctx, sql, topicID).Scan(&floor)
	return floor, err
}

// partitionExpired reports whether a partition's newest row is past ttl.
func (d *MaintenanceDatastore) partitionExpired(ctx context.Context, topicID int64, n int64, ttl time.Duration) (bool, error) {
	sql := fmt.Sprintf(`
		SELECT created_at FROM %s
		ORDER BY id DESC -- rides the PK index; id order approx time order, no created_at index needed
		LIMIT 1;
	`, topic.MessageLogPartitionTable(topicID, n))

	var newest time.Time
	err := d.Datastore.Pool.QueryRow(ctx, sql).Scan(&newest)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil // empty -- nothing to judge, so not expired
	}
	if err != nil {
		return false, err
	}

	return time.Since(newest) >= ttl, nil
}

// SweepExpiredPartitions drains the ttl-expired prefix of every surviving
// partition -- covers the low-volume tail that never fills a partition wide
// enough to earn a whole-partition drop.
func (d *MaintenanceDatastore) SweepExpiredPartitions(ctx context.Context, topicID int64, partitionSize int64, ttl time.Duration, allowDropPastCommitted bool, batchSize int, disableDeliveryLog bool) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.sweepExpiredPartitions(ctx, topicID, partitionSize, ttl, allowDropPastCommitted, batchSize, disableDeliveryLog)
	})
}

func (d *MaintenanceDatastore) sweepExpiredPartitions(ctx context.Context, topicID int64, partitionSize int64, ttl time.Duration, allowDropPastCommitted bool, batchSize int, disableDeliveryLog bool) error {
	if ttl <= 0 {
		return nil // retention disabled
	}

	partitions, err := d.existingPartitions(ctx, topicID)
	if err != nil {
		return err
	}

	floor, err := d.cursorFloor(ctx, topicID)
	if err != nil {
		return err
	}
	if allowDropPastCommitted {
		floor = nil
	}

	cutoff := time.Now().Add(-ttl)

	// caps a full drain to break any potential infinite loops
	maxBatches := int((partitionSize + int64(batchSize) - 1) / int64(batchSize))

	for _, n := range partitions { // every partition, independently -- one backlog can't block the rest
		for range maxBatches {
			swept, err := d.sweepBatch(ctx, topicID, n, cutoff, floor, batchSize, disableDeliveryLog)
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
func (d *MaintenanceDatastore) sweepBatch(ctx context.Context, topicID int64, n int64, cutoff time.Time, floor *int64, batchSize int, disableDeliveryLog bool) (int, error) {
	tx, err := d.Datastore.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	// sweptRow is sweepBatch's own RETURNING shape -- CompactionKey only exists
	// to tell whether the compaction_head cleanup is worth running at all.
	type sweptRow struct {
		Id            int64   `db:"id"`
		CompactionKey *string `db:"compaction_key"`
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
	`, topic.MessageLogPartitionTable(topicID, n), topic.MessageLogPartitionTable(topicID, n))

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
		// otherwise these delivery rows (mostly 'dead' DLQ) would join to nothing and park forever.
		orphanSql := fmt.Sprintf(`
			DELETE FROM %s
			WHERE message_id = ANY($1);
		`, topic.DeliveryTable(topicID))
		if _, err := tx.Exec(ctx, orphanSql, ids); err != nil {
			return 0, err
		}

		if !disableDeliveryLog {
			orphanLogSql := fmt.Sprintf(`
				DELETE FROM %s
				WHERE message_id = ANY($1);
			`, topic.DeliveryLogTable(topicID))
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
		if _, err := tx.Exec(ctx, orphanKeySql, topicID, ids); err != nil {
			return 0, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}

	if len(ids) > 0 {
		d.Logger.DebugContext(ctx, "swept expired rows", "topic_id", topicID, "partition", n, "swept", len(ids), "batch_size", batchSize)
	}

	return len(ids), nil
}

// dropPartition removes the partition and its delivery/delivery_log rows in
// one transaction.
func (d *MaintenanceDatastore) dropPartition(ctx context.Context, topicID int64, n int64, partitionSize int64, disableDeliveryLog bool) error {
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
	`, topic.DeliveryTable(topicID))
	if _, err := tx.Exec(ctx, orphanSql, low, high); err != nil {
		return err
	}

	if !disableDeliveryLog {
		orphanLogSql := fmt.Sprintf(`
			DELETE FROM %s
			WHERE message_id >= $1
				AND message_id < $2;
		`, topic.DeliveryLogTable(topicID))
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
	`, topic.MessageLogPartitionTable(topicID, n))

	if _, err := tx.Exec(ctx, dropSql); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// SweepExpiredIdempotencyKeys drains idempotency_key rows older than ttl for this topic.
func (d *MaintenanceDatastore) SweepExpiredIdempotencyKeys(ctx context.Context, topicID int64, ttl time.Duration, batchSize int) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.sweepExpiredIdempotencyKeys(ctx, topicID, ttl, batchSize)
	})
}

func (d *MaintenanceDatastore) sweepExpiredIdempotencyKeys(ctx context.Context, topicID int64, ttl time.Duration, batchSize int) error {
	// defensive only, not a keep-forever switch like RetentionTTL:
	// topic.Config defaults an unset IdempotencyKeyTTL to 1h at registration,
	// and there's no supported way to opt idempotency_key rows out of
	// being swept
	if ttl <= 0 {
		return nil
	}

	cutoff := time.Now().Add(-ttl)

	// protect against any potential infinite loops
	const maxIdempotencyKeySweepBatches = 1000
	for range maxIdempotencyKeySweepBatches {
		swept, err := d.sweepIdempotencyKeysBatch(ctx, topicID, cutoff, batchSize)
		if err != nil {
			return err
		}
		if swept < batchSize {
			break // ran out of expired rows
		}
	}

	return nil
}

// sweepIdempotencyKeysBatch deletes up to batchSize expired rows from this
// topic's own idempotency_key_<id> table. created_at (not idempotency_key)
// is the cutoff column -- a caller-supplied key isn't guaranteed to be a
// time-ordered UUIDv7 the way the auto-generated default is, so only the
// server-assigned timestamp is trustworthy for this.
func (d *MaintenanceDatastore) sweepIdempotencyKeysBatch(ctx context.Context, topicID int64, cutoff time.Time, batchSize int) (int, error) {
	sql := fmt.Sprintf(`
		DELETE FROM %s
		WHERE idempotency_key IN (
			SELECT idempotency_key FROM %s
			WHERE created_at < $1
			LIMIT $2
		);
	`, topic.IdempotencyKeyTable(topicID), topic.IdempotencyKeyTable(topicID))

	tag, err := d.Datastore.Pool.Exec(ctx, sql, cutoff, batchSize)
	if err != nil {
		return 0, err
	}

	return int(tag.RowsAffected()), nil
}

// SweepExpiredKeyLeases deletes this topic's expired key_lease rows.
// A crashed consumer leaves its expired row behind:
//   - the key gets another message -> that claim takes the row over
//   - the key never does -> the row sits forever, only this sweep removes it
func (d *MaintenanceDatastore) SweepExpiredKeyLeases(ctx context.Context, topicID int64, batchSize int) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.sweepExpiredKeyLeases(ctx, topicID, batchSize)
	})
}

func (d *MaintenanceDatastore) sweepExpiredKeyLeases(ctx context.Context, topicID int64, batchSize int) error {
	// protect against any potential infinite loops
	const maxKeyLeaseSweepBatches = 1000
	for range maxKeyLeaseSweepBatches {
		swept, err := d.sweepKeyLeasesBatch(ctx, topicID, batchSize)
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
func (d *MaintenanceDatastore) sweepKeyLeasesBatch(ctx context.Context, topicID int64, batchSize int) (int, error) {
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

	tag, err := d.Datastore.Pool.Exec(ctx, sql, topicID, batchSize)
	if err != nil {
		return 0, err
	}

	return int(tag.RowsAffected()), nil
}
