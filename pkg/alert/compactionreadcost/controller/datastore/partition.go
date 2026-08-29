package datastore

import (
	"context"

	iTopic "github.com/agentstax/vulkan/internal/topic"
)

// PartitionCount is the number of partitions on the topic's message log.
func (d *CompactionReadCostDatastore) PartitionCount(ctx context.Context, topicId int64) (int64, error) {
	var count int64
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		count, err = d.partitionCount(ctx, topicId)
		return err
	})
	return count, err
}

func (d *CompactionReadCostDatastore) partitionCount(ctx context.Context, topicId int64) (int64, error) {
	sql := `
		-- vulkan: compactionreadcost.partitionCount
		SELECT count(*) FROM pg_inherits WHERE inhparent = to_regclass($1);
	`
	var count int64
	err := d.Datastore.Pool.QueryRow(ctx, sql, iTopic.MessageLogTable(topicId)).Scan(&count)
	return count, err
}
