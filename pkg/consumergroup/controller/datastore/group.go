package datastore

import (
	"context"
	"errors"
	"fmt"

	iTopic "github.com/agentstax/vulkan/internal/topic"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// GetGroup resolves a consumer group by its owning topic and name.
// Returns (nil, nil) if the group is not registered on that topic.
func (d *ConsumerGroupDatastore) GetGroup(ctx context.Context, topicId int64, name string) (*GroupData, error) {
	var group *GroupData
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		group, err = d.getGroup(ctx, d.Datastore.Pool, topicId, name)
		return err
	})
	return group, err
}

func (d *ConsumerGroupDatastore) getGroup(ctx context.Context, q datastore.Querier, topicId int64, name string) (*GroupData, error) {
	sql := `
		-- vulkan: consumergroup.getGroup
		SELECT id, topic_id, name, created_at
		FROM consumer_group_config
		WHERE topic_id = $1 AND name = $2;
	`
	var group GroupData
	err := q.QueryRow(ctx, sql, topicId, name).Scan(&group.Id, &group.TopicId, &group.Name, &group.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &group, nil
}

// RegisterGroup registers the group and its cursor if it doesn't exist.
func (d *ConsumerGroupDatastore) RegisterGroup(ctx context.Context, topicId int64, name string) (*GroupData, error) {
	var group *GroupData
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		group, err = d.registerGroup(ctx, topicId, name)
		return err
	})
	return group, err
}

// registerGroup registers behind a per-(topic,name) advisory lock, NOT ON CONFLICT.
// This is to prevent race condition errors between two concurrent calls.
func (d *ConsumerGroupDatastore) registerGroup(ctx context.Context, topicId int64, name string) (*GroupData, error) {
	tx, err := d.Datastore.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// private getGroup, not GetGroup -- otherwise would have nested retries.
	found, err := d.getGroup(ctx, tx, topicId, name)
	if err != nil {
		return nil, err
	}
	if found != nil {
		return found, nil
	}

	// txn-scoped, per-(topic, name) -- auto-released at commit/rollback
	if _, err := tx.Exec(ctx, `-- vulkan: consumergroup.registerGroup
SELECT pg_advisory_xact_lock(hashtext(format('consumer_group:%s:%s', $1::bigint, $2::text)));`, topicId, name); err != nil {
		return nil, err
	}

	// re-check under the lock -- a racing registration may have committed while we waited
	found, err = d.getGroup(ctx, tx, topicId, name)
	if err != nil {
		return nil, err
	}
	if found != nil {
		return found, nil
	}

	insertSql := `
		-- vulkan: consumergroup.registerGroup
		INSERT INTO consumer_group_config (topic_id, name)
		VALUES ($1, $2)
		RETURNING id, topic_id, name, created_at;
	`
	var group GroupData
	if err := tx.QueryRow(ctx, insertSql, topicId, name).Scan(&group.Id, &group.TopicId, &group.Name, &group.CreatedAt); err != nil {
		// 23503 = the topic_id FK -- name the real problem, not the constraint
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return nil, topic.ErrTopicNotFound.With("topic_id", topicId)
		}
		return nil, err
	}

	cursorSql := fmt.Sprintf(`
		-- vulkan: consumergroup.registerGroup
		INSERT INTO %s (consumer_group_id)
		VALUES ($1);
	`, iTopic.ConsumerGroupCursorTable(topicId))
	if _, err := tx.Exec(ctx, cursorSql, group.Id); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	d.Logger.InfoContext(ctx, "consumer group registered (created)", "group", group.Name, "topic_id", group.TopicId, "group_id", group.Id)
	return &group, nil
}

// DeleteGroup deletes the group and every row it owns in one transaction.
func (d *ConsumerGroupDatastore) DeleteGroup(ctx context.Context, topicId int64, groupId int64, name string) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.deleteGroup(ctx, topicId, groupId, name)
	})
}

func (d *ConsumerGroupDatastore) deleteGroup(ctx context.Context, topicId int64, groupId int64, name string) error {
	tx, err := d.Datastore.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// no cascade -- nothing references the per-topic claim_lease table
	leaseSql := fmt.Sprintf(`-- vulkan: consumergroup.deleteGroup
DELETE FROM %s WHERE consumer_group_id = $1;`, iTopic.ClaimLeaseTable(topicId))
	if _, err := tx.Exec(ctx, leaseSql, groupId); err != nil {
		return err
	}

	// no cascade -- nothing references the per-topic key_lease table
	keyLeaseSql := fmt.Sprintf(`-- vulkan: consumergroup.deleteGroup
DELETE FROM %s WHERE consumer_group_id = $1;`, iTopic.KeyLeaseTable(topicId))
	if _, err := tx.Exec(ctx, keyLeaseSql, groupId); err != nil {
		return err
	}

	// no cascade -- nothing references the per-topic exception_queue table
	deliverySql := fmt.Sprintf(`-- vulkan: consumergroup.deleteGroup
DELETE FROM %s WHERE consumer_group_id = $1;`, iTopic.ExceptionQueueTable(topicId))
	if _, err := tx.Exec(ctx, deliverySql, groupId); err != nil {
		return err
	}

	// no cascade -- nothing references the per-topic delivery_log table
	deliveryLogSql := fmt.Sprintf(`-- vulkan: consumergroup.deleteGroup
DELETE FROM %s WHERE consumer_group_id = $1;`, iTopic.DeliveryLogTable(topicId))
	if _, err := tx.Exec(ctx, deliveryLogSql, groupId); err != nil {
		return err
	}

	// cascades: cursor, binding, migration_log, group-owned worker and
	// cron_job rows; worker_instance follows its worker
	if _, err := tx.Exec(ctx, `-- vulkan: consumergroup.deleteGroup
DELETE FROM consumer_group_config WHERE id = $1;`, groupId); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}

	d.Logger.InfoContext(ctx, "consumer group deleted", "group", name, "topic_id", topicId, "group_id", groupId)
	return nil
}
