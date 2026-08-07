package datastore

import (
	"context"
	"fmt"
	"github.com/agentstax/vulkan/internal/topic"
	"time"
)

func (s *ConsumerGroupSnapshot) GroupLag() GroupLag {
	return GroupLag{
		ConsumerGroup:    s.ConsumerGroup,
		Committed:        s.Committed,
		Head:             s.Head,
		Lag:              max(s.Backlog, 0),
		ParkedExceptions: s.ReadyExceptions + s.InflightExceptions + s.DeferredExceptions,
	}
}

// ConsumerGroupSnapshot is the current picture for (topicID, consumerGroup),
// queried live from Postgres -- works cold, nothing needs to be running.
func (d *MetricsDatastore) ConsumerGroupSnapshot(ctx context.Context, topicID int64, consumerGroup string) (*ConsumerGroupSnapshot, error) {
	var snapshot *ConsumerGroupSnapshot
	err := d.Retry.Wrap(ctx, func() error {
		var err error
		snapshot, err = d.consumerGroupSnapshot(ctx, topicID, consumerGroup)
		return err
	})
	return snapshot, err
}

func (d *MetricsDatastore) consumerGroupSnapshot(ctx context.Context, topicID int64, consumerGroup string) (*ConsumerGroupSnapshot, error) {
	var consumerGroupID int64
	if err := d.Datastore.Pool.QueryRow(ctx, `SELECT id FROM consumer_group WHERE topic_id = $1 AND name = $2;`, topicID, consumerGroup).Scan(&consumerGroupID); err != nil {
		return nil, err
	}

	sql := fmt.Sprintf(`
		SELECT
			c.claimed,
			c.committed,
			COALESCE((
				SELECT MAX(id)
				FROM %[1]s
			), 0) AS head,
			COALESCE((
				SELECT COUNT(*)
				FROM %[2]s
				WHERE consumer_group_id = $1 AND status = 'ready'
			), 0) AS ready_exceptions,
			COALESCE((
				SELECT COUNT(*)
				FROM %[2]s
				WHERE consumer_group_id = $1 AND status = 'inflight'
			), 0) AS inflight_exceptions,
			COALESCE((
				SELECT COUNT(*)
				FROM %[2]s
				WHERE consumer_group_id = $1 AND status = 'deferred'
			), 0) AS deferred_exceptions,
			COALESCE((
				SELECT COUNT(*)
				FROM %[2]s
				WHERE consumer_group_id = $1 AND status = 'dead'
			), 0) AS dead_exceptions,
			(
				SELECT MIN(created_at)
				FROM %[2]s
				WHERE consumer_group_id = $1 AND status IN ('ready', 'inflight', 'deferred')
			) AS oldest_unacked_at,
			COALESCE((
				SELECT COUNT(*)
				FROM lease
				WHERE consumer_group_id = $1
			), 0) AS open_leases
		FROM cursor c
		WHERE c.consumer_group_id = $1;
	`, topic.MessageLogTable(topicID), topic.DeliveryTable(topicID))

	var s ConsumerGroupSnapshot
	var oldestUnackedAt *time.Time
	err := d.Datastore.Pool.QueryRow(ctx, sql, consumerGroupID).Scan(
		&s.Claimed,
		&s.Committed,
		&s.Head,
		&s.ReadyExceptions,
		&s.InflightExceptions,
		&s.DeferredExceptions,
		&s.DeadExceptions,
		&oldestUnackedAt,
		&s.OpenLeases,
	)
	if err != nil {
		return nil, err
	}

	s.ConsumerGroup = consumerGroup
	s.Backlog = s.Head - s.Committed
	s.Inflight = s.Claimed - s.Committed
	if oldestUnackedAt != nil {
		s.OldestUnackedAge = time.Since(*oldestUnackedAt)
	}

	return &s, nil
}

// ListConsumerGroups is every group registered on topicID -- the groups a
// health view must account for before the topic can be considered drained.
func (d *MetricsDatastore) ListConsumerGroups(ctx context.Context, topicID int64) ([]string, error) {
	var groups []string
	err := d.Retry.Wrap(ctx, func() error {
		var err error
		groups, err = d.listConsumerGroups(ctx, topicID)
		return err
	})
	return groups, err
}

func (d *MetricsDatastore) listConsumerGroups(ctx context.Context, topicID int64) ([]string, error) {
	sql := `
		SELECT name
		FROM consumer_group
		WHERE topic_id = $1 ORDER BY name;
	`

	rows, err := d.Datastore.Pool.Query(ctx, sql, topicID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []string
	for rows.Next() {
		var g string
		if err := rows.Scan(&g); err != nil {
			return nil, err
		}
		groups = append(groups, g)
	}
	return groups, rows.Err()
}
