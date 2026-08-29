package datastore

import (
	"context"

	iTopic "github.com/agentstax/vulkan/internal/topic"
)

// locksPerPartition: a partition owns 5 lockable relations -- table, pkey
// index, message_key index, TOAST table, TOAST index -- and dropping the
// parent locks them all at once.
const locksPerPartition = 5

// PartitionCount is the number of partitions on the topic's message log.
func (d *PartitionCountDatastore) PartitionCount(ctx context.Context, topicId int64) (int64, error) {
	var count int64
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		count, err = d.partitionCount(ctx, topicId)
		return err
	})
	return count, err
}

func (d *PartitionCountDatastore) partitionCount(ctx context.Context, topicId int64) (int64, error) {
	sql := `
		-- vulkan: partitioncount.partitionCount
		SELECT count(*) FROM pg_inherits WHERE inhparent = to_regclass($1);
	`
	var count int64
	err := d.Datastore.Pool.QueryRow(ctx, sql, iTopic.MessageLogTable(topicId)).Scan(&count)
	return count, err
}

// PartitionLockCeiling is the partition count at which a DROP/Destroy risks
// "out of shared memory".
func (d *PartitionCountDatastore) PartitionLockCeiling(ctx context.Context) (int64, error) {
	var ceiling int64
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		ceiling, err = d.partitionLockCeiling(ctx)
		return err
	})
	return ceiling, err
}

func (d *PartitionCountDatastore) partitionLockCeiling(ctx context.Context) (int64, error) {
	// the product is the lock table's total size, fixed at server start
	sql := `
		-- vulkan: partitioncount.partitionLockCeiling
		SELECT current_setting('max_locks_per_transaction')::bigint
			* (current_setting('max_connections')::bigint
				+ current_setting('max_prepared_transactions')::bigint);
	`
	var size int64
	if err := d.Datastore.Pool.QueryRow(ctx, sql).Scan(&size); err != nil {
		return 0, err
	}
	return size / locksPerPartition, nil
}
