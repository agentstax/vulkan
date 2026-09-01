package datastore

import (
	"context"
	"fmt"

	"github.com/agentstax/vulkan/pkg/topic"
)

// IsEmpty reports whether the topic's log holds any row at all.
func (d *TopicDatastore) IsEmpty(ctx context.Context, topicId int64) (bool, error) {
	var empty bool
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		empty, err = d.isEmpty(ctx, topicId)
		return err
	})
	return empty, err
}

func (d *TopicDatastore) isEmpty(ctx context.Context, topicId int64) (bool, error) {
	// Partition-pruned and LIMIT 1'd, so it stays cheap regardless of topic size.
	sql := fmt.Sprintf(`
		-- vulkan: topic.isEmpty
		SELECT EXISTS (SELECT 1 FROM %s LIMIT 1);
	`, topic.MessageLogTable(topicId))
	var notEmpty bool
	if err := d.Datastore.Pool.QueryRow(ctx, sql).Scan(&notEmpty); err != nil {
		return false, err
	}
	return !notEmpty, nil
}

func (d *TopicDatastore) Delete(ctx context.Context, topicId int64, name string) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.delete(ctx, topicId, name)
	})
}

func (d *TopicDatastore) delete(ctx context.Context, topicId int64, name string) error {
	if err := d.drainPartitions(ctx, topicId); err != nil {
		return err
	}

	tx, err := d.Datastore.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		-- vulkan: topic.delete
		DELETE FROM topic_config WHERE id = $1;
	`, topicId); err != nil {
		return err
	}

	// the now-empty message_log parent and every other per-topic table
	for _, table := range []string{
		topic.MessageLogTable(topicId),
		topic.ExceptionQueueTable(topicId),
		topic.DeliveryLogTable(topicId),
		topic.IdempotencyKeyTable(topicId),
		topic.ConsumerGroupCursorTable(topicId),
		topic.ClaimLeaseTable(topicId),
		topic.MessageKeyLeaseTable(topicId),
		topic.CompactionHeadTable(topicId),
		topic.BindingConfigTable(topicId),
		topic.BindingConfigLogTable(topicId),
	} {
		dropSql := fmt.Sprintf(`
			-- vulkan: topic.delete
			DROP TABLE IF EXISTS %s;`, table)
		if _, err := tx.Exec(ctx, dropSql); err != nil {
			return err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}

	d.Logger.InfoContext(ctx, "topic destroyed", "topic", name, "topic_id", topicId)
	return nil
}
