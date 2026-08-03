package datastore

import (
	"context"
	"errors"
	"fmt"

	iTopic "github.com/agentstax/vulkan/internal/topic"
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
	sql := fmt.Sprintf(`SELECT EXISTS (SELECT 1 FROM %s LIMIT 1);`, iTopic.MessageLogTable(topicId))
	var notEmpty bool
	if err := d.Datastore.Pool.QueryRow(ctx, sql).Scan(&notEmpty); err != nil {
		return false, err
	}
	return !notEmpty, nil
}

func (d *TopicDatastore) DeleteTopic(ctx context.Context, topicId int64, name string) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.deleteTopic(ctx, topicId, name)
	})
}

func (d *TopicDatastore) deleteTopic(ctx context.Context, topicId int64, name string) error {
	if err := d.drainPartitions(ctx, iTopic.MessageLogTable(topicId)); err != nil {
		if errors.Is(err, errPartitionsRemain) {
			return fmt.Errorf("topic %s: %w -- a producer is likely still writing; stop producers and call Destroy again", name, err)
		}
		return err
	}

	tx, err := d.Datastore.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	leaseSql := `
		DELETE FROM lease
		WHERE consumer_group_id IN (SELECT id FROM consumer_group WHERE topic_id = $1);
	`
	if _, err := tx.Exec(ctx, leaseSql, topicId); err != nil {
		return err
	}

	keyLeaseSql := `
		DELETE FROM key_lease
		WHERE consumer_group_id IN (SELECT id FROM consumer_group WHERE topic_id = $1);
	`
	if _, err := tx.Exec(ctx, keyLeaseSql, topicId); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `DELETE FROM topic WHERE id = $1;`, topicId); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `DELETE FROM compaction_head WHERE topic_id = $1;`, topicId); err != nil {
		return err
	}

	// the now-empty parent, delivery_<id>, and idempotency_key_<id>
	dropTableSql := fmt.Sprintf(`DROP TABLE IF EXISTS %s;`, iTopic.MessageLogTable(topicId))
	if _, err := tx.Exec(ctx, dropTableSql); err != nil {
		return err
	}
	dropDeliverySql := fmt.Sprintf(`DROP TABLE IF EXISTS %s;`, iTopic.DeliveryTable(topicId))
	if _, err := tx.Exec(ctx, dropDeliverySql); err != nil {
		return err
	}
	dropDeliveryLogSql := fmt.Sprintf(`DROP TABLE IF EXISTS %s;`, iTopic.DeliveryLogTable(topicId))
	if _, err := tx.Exec(ctx, dropDeliveryLogSql); err != nil {
		return err
	}
	dropIdempotencyKeySql := fmt.Sprintf(`DROP TABLE IF EXISTS %s;`, iTopic.IdempotencyKeyTable(topicId))
	if _, err := tx.Exec(ctx, dropIdempotencyKeySql); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}

	d.Logger.WarnContext(ctx, "topic destroyed", "topic", name, "topic_id", topicId)
	return nil
}
