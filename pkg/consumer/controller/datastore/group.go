package datastore

import (
	"context"
	"errors"
	"fmt"

	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// GetGroup resolves a consumer group by its owning topic and name.
// Returns (nil, nil) if the group is not registered on that topic.
func (d *ConsumerDatastore) GetGroup(ctx context.Context, topicID int64, name string) (*GroupData, error) {
	var group *GroupData
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		group, err = d.getGroup(ctx, d.Datastore.Pool, topicID, name)
		return err
	})
	return group, err
}

func (d *ConsumerDatastore) getGroup(ctx context.Context, q datastore.Querier, topicID int64, name string) (*GroupData, error) {
	sql := `
		SELECT id, topic_id, name, created_at
		FROM consumer_group
		WHERE topic_id = $1 AND name = $2;
	`
	var group GroupData
	err := q.QueryRow(ctx, sql, topicID, name).Scan(&group.Id, &group.TopicId, &group.Name, &group.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &group, nil
}

// RegisterGroup registers the group and its cursor if it doesn't exist.
func (d *ConsumerDatastore) RegisterGroup(ctx context.Context, topicID int64, name string) (*GroupData, error) {
	var group *GroupData
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		group, err = d.registerGroup(ctx, topicID, name)
		return err
	})
	return group, err
}

// registerGroup registers behind a per-(topic,name) advisory lock, NOT ON CONFLICT.
// This is to prevent race condition errors between two concurrent calls.
func (d *ConsumerDatastore) registerGroup(ctx context.Context, topicID int64, name string) (*GroupData, error) {
	tx, err := d.Datastore.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// private getGroup, not GetGroup -- otherwise would have nested retries.
	found, err := d.getGroup(ctx, tx, topicID, name)
	if err != nil {
		return nil, err
	}
	if found != nil {
		return found, nil
	}

	// txn-scoped, per-(topic, name) -- auto-released at commit/rollback
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext(format('consumer_group:%s:%s', $1::bigint, $2::text)));`, topicID, name); err != nil {
		return nil, err
	}
	// re-check under the lock -- a racing registration may have committed while we waited
	found, err = d.getGroup(ctx, tx, topicID, name)
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
	if err := tx.QueryRow(ctx, insertSql, topicID, name).Scan(&group.Id, &group.TopicId, &group.Name, &group.CreatedAt); err != nil {
		// 23503 = the topic_id FK -- name the real problem, not the constraint
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return nil, fmt.Errorf("topic %d is not registered -- register it with MessageAdmin.RegisterTopic first", topicID)
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
