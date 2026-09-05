package datastore

import (
	"context"
	"errors"
	"fmt"

	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/jackc/pgx/v5"
)

// ConsumerGroupSnapshot is the current cursor/delivery/lease picture for
// (topicId, consumerGroupId).
func (d *MetricsDatastore) ConsumerGroupSnapshot(ctx context.Context, topicId int64, consumerGroupId int64) (*ConsumerGroupSnapshotRow, error) {
	var snapshot *ConsumerGroupSnapshotRow
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		snapshot, err = d.consumerGroupSnapshot(ctx, topicId, consumerGroupId)
		return err
	})
	return snapshot, err
}

func (d *MetricsDatastore) consumerGroupSnapshot(ctx context.Context, topicId int64, consumerGroupId int64) (*ConsumerGroupSnapshotRow, error) {
	sql := fmt.Sprintf(`
		-- vulkan: metrics.consumerGroupSnapshot
		SELECT
			c.claimed,
			c.committed,
			COALESCE((
				SELECT MAX(id)
				FROM %[1]s.%[2]s
			), 0) AS head,
			COALESCE((
				SELECT COUNT(*)
				FROM %[1]s.%[3]s
				WHERE consumer_group_id = $1 AND status = 'ready'
			), 0) AS ready_exceptions,
			COALESCE((
				SELECT COUNT(*)
				FROM %[1]s.%[3]s
				WHERE consumer_group_id = $1 AND status = 'inflight'
			), 0) AS inflight_exceptions,
			COALESCE((
				SELECT COUNT(*)
				FROM %[1]s.%[3]s
				WHERE consumer_group_id = $1 AND status = 'deferred'
			), 0) AS deferred_exceptions,
			COALESCE((
				SELECT COUNT(*)
				FROM %[1]s.%[3]s
				WHERE consumer_group_id = $1 AND status = 'dead'
			), 0) AS dead_exceptions,
			(
				SELECT MIN(created_at)
				FROM %[1]s.%[3]s
				WHERE consumer_group_id = $1 AND status IN ('ready', 'inflight', 'deferred')
			) AS oldest_unresolved_at,
			COALESCE((
				SELECT COUNT(*)
				FROM %[1]s.%[4]s
				WHERE consumer_group_id = $1
			), 0) AS open_leases
		FROM %[1]s.%[5]s c
		WHERE c.consumer_group_id = $1;
	`, d.Datastore.Schema, topic.MessageLogTable(topicId), topic.ExceptionQueueTable(topicId), topic.ClaimLeaseTable(topicId), topic.ConsumerGroupCursorTable(topicId))

	var data ConsumerGroupSnapshotRow
	err := d.Datastore.Pool.QueryRow(ctx, sql, consumerGroupId).Scan(
		&data.Claimed,
		&data.Committed,
		&data.Head,
		&data.ReadyExceptions,
		&data.InflightExceptions,
		&data.DeferredExceptions,
		&data.DeadExceptions,
		&data.OldestUnresolvedAt,
		&data.OpenLeases,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &data, nil
}

// ListConsumerGroups is every group's id and name on topicId, ordered by name.
func (d *MetricsDatastore) ListConsumerGroups(ctx context.Context, topicId int64) ([]ConsumerGroupIdentityRow, error) {
	var groups []ConsumerGroupIdentityRow
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		groups, err = d.listConsumerGroups(ctx, topicId)
		return err
	})
	return groups, err
}

func (d *MetricsDatastore) listConsumerGroups(ctx context.Context, topicId int64) ([]ConsumerGroupIdentityRow, error) {
	sql := fmt.Sprintf(`
		-- vulkan: metrics.listConsumerGroups
		SELECT id, name
		FROM %[1]s.consumer_group_config
		WHERE topic_id = $1
		ORDER BY name;
	`, d.Datastore.Schema)
	rows, err := d.Datastore.Pool.Query(ctx, sql, topicId)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[ConsumerGroupIdentityRow])
}
