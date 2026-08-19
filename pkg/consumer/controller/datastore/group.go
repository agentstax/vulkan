package datastore

import (
	"context"
	"errors"
	"fmt"

	iTopic "github.com/agentstax/vulkan/internal/topic"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// GetGroup resolves a consumer group by its owning topic and name.
// Returns (nil, nil) if the group is not registered on that topic.
func (d *ConsumerDatastore) GetGroup(ctx context.Context, topicId int64, name string) (*GroupData, error) {
	var group *GroupData
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		group, err = d.getGroup(ctx, d.Datastore.Pool, topicId, name)
		return err
	})
	return group, err
}

func (d *ConsumerDatastore) getGroup(ctx context.Context, q datastore.Querier, topicId int64, name string) (*GroupData, error) {
	sql := `
		SELECT id, topic_id, name, created_at
		FROM consumer_group
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
func (d *ConsumerDatastore) RegisterGroup(ctx context.Context, topicId int64, name string) (*GroupData, error) {
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
func (d *ConsumerDatastore) registerGroup(ctx context.Context, topicId int64, name string) (*GroupData, error) {
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
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext(format('consumer_group:%s:%s', $1::bigint, $2::text)));`, topicId, name); err != nil {
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
		INSERT INTO consumer_group (topic_id, name)
		VALUES ($1, $2)
		RETURNING id, topic_id, name, created_at;
	`
	var group GroupData
	if err := tx.QueryRow(ctx, insertSql, topicId, name).Scan(&group.Id, &group.TopicId, &group.Name, &group.CreatedAt); err != nil {
		// 23503 = the topic_id FK -- name the real problem, not the constraint
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return nil, fmt.Errorf("topic %d is not registered -- register it with MessageAdmin.RegisterTopic first", topicId)
		}
		return nil, err
	}

	cursorSql := `
		INSERT INTO cursor (consumer_group_id)
		VALUES ($1);
	`
	if _, err := tx.Exec(ctx, cursorSql, group.Id); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	d.Logger.InfoContext(ctx, "consumer group registered (created)", "consumer_group", group.Name, "topic_id", group.TopicId, "group_id", group.Id)
	return &group, nil
}

// DeleteGroup deletes the group and every row it owns in one transaction.
func (d *ConsumerDatastore) DeleteGroup(ctx context.Context, topicId int64, groupId int64, name string) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.deleteGroup(ctx, topicId, groupId, name)
	})
}

func (d *ConsumerDatastore) deleteGroup(ctx context.Context, topicId int64, groupId int64, name string) error {
	tx, err := d.Datastore.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// no cascade -- nothing references lease
	if _, err := tx.Exec(ctx, `DELETE FROM lease WHERE consumer_group_id = $1;`, groupId); err != nil {
		return err
	}

	// no cascade -- nothing references key_lease
	if _, err := tx.Exec(ctx, `DELETE FROM key_lease WHERE consumer_group_id = $1;`, groupId); err != nil {
		return err
	}

	// no cascade -- nothing references the per-topic delivery table
	deliverySql := fmt.Sprintf(`DELETE FROM %s WHERE consumer_group_id = $1;`, iTopic.DeliveryTable(topicId))
	if _, err := tx.Exec(ctx, deliverySql, groupId); err != nil {
		return err
	}

	// no cascade -- nothing references the per-topic delivery_log table
	deliveryLogSql := fmt.Sprintf(`DELETE FROM %s WHERE consumer_group_id = $1;`, iTopic.DeliveryLogTable(topicId))
	if _, err := tx.Exec(ctx, deliveryLogSql, groupId); err != nil {
		return err
	}

	// cascades: cursor, binding, migration_log, group-owned worker and
	// cron_job rows; worker_instance follows its worker
	if _, err := tx.Exec(ctx, `DELETE FROM consumer_group WHERE id = $1;`, groupId); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}

	d.Logger.WarnContext(ctx, "consumer group deleted", "consumer_group", name, "topic_id", topicId, "group_id", groupId)
	return nil
}
