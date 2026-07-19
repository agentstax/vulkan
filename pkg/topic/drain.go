package topic

import (
	"context"
	"errors"
	"fmt"
)

// Dropping one partition locks ~5 relations:
//   - the partition itself
//   - its pkey index
//   - its compaction_key index
//   - its TOAST table + TOAST index
//
// Locks come from a pool sized ONCE at server start
//   - (Default) max_locks_per_transaction * max_connections = 64 * 100 = 6400 stock
//
// So a topic's log can't be destroyed with one DROP of the partitioned
// parent: that single transaction holds every partition's 5 slots at once
// and fails with "out of shared memory" past ~1000 partitions. Instead
// drainPartitions drops batches of this size, each in its own transaction
// -- 100 partitions hold ~500 slots, under 10% of the stock pool.
const dropPartitionBatchSize = 100

var errPartitionsRemain = errors.New("partitions remain")

// Batched partition drain: removes a partitioned table's partitions a
// batch at a time so no single transaction holds more than a batch's
// worth of lock slots, leaving the parent empty for a cheap final DROP.
func (d *topicDatastore) drainPartitions(ctx context.Context, parentTableName string) error {
	countSql := `SELECT count(*) FROM pg_inherits WHERE inhparent = to_regclass($1);`
	var partitionCount int64
	if err := d.Datastore.Pool.QueryRow(ctx, countSql, parentTableName).Scan(&partitionCount); err != nil {
		return err
	}
	// nothing to drain (or the parent doesn't exist at all)
	if partitionCount == 0 {
		return nil
	}

	// bounded -- a concurrent producer or janitor can recreate partitions mid-drain,
	// which would cause this to loop forever
	passLimit := partitionCount/dropPartitionBatchSize + 3

	for range passLimit {
		partitions, err := d.listPartitions(ctx, parentTableName)
		if err != nil {
			return err
		}
		// successfully deleted all partitions -> exit
		if len(partitions) == 0 {
			return nil
		}
		if err := d.dropPartitionBatch(ctx, partitions); err != nil {
			return err
		}
	}
	// went past passLimit
	return fmt.Errorf("%w: %s after %d drop passes", errPartitionsRemain, parentTableName, passLimit)
}

func (d *topicDatastore) listPartitions(ctx context.Context, parentTableName string) ([]string, error) {
	// to_regclass, not ::regclass -- a missing parent yields NULL, not error
	sql := `
		SELECT c.relname
		FROM pg_inherits i
		JOIN pg_class c ON c.oid = i.inhrelid
		WHERE i.inhparent = to_regclass($1)
		LIMIT $2;
	`

	rows, err := d.Datastore.Pool.Query(ctx, sql, parentTableName, dropPartitionBatchSize)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var partitions []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		partitions = append(partitions, name)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return partitions, nil
}

func (d *topicDatastore) dropPartitionBatch(ctx context.Context, partitions []string) error {
	tx, err := d.Datastore.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	for _, partition := range partitions {
		// IF EXISTS -- a live retention janitor can drop an expired
		// partition between the listPartitions read and here
		dropSql := fmt.Sprintf(`DROP TABLE IF EXISTS %s;`, partition)
		if _, err := tx.Exec(ctx, dropSql); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}
