package datastore

import (
	"context"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/internal/topic"
)

// ConsumerGroupSnapshot is the live, DB-truth picture of one (group, topic)'s
// queue -- answers "what's true right now" for state that multiple consumer
// processes share (cursor/delivery/lease), which no in-process counter can.
type ConsumerGroupSnapshot struct {
	ConsumerGroup string // whose picture this is

	Head      int64 // highest message id ever appended -- the log frontier
	Claimed   int64 // cursor.claimed -- the read frontier
	Committed int64 // cursor.committed -- everything <= this is done/dead

	Backlog  int64 // Head - Committed -- the waterline gap
	Inflight int64 // Claimed - Committed -- claimed but not yet resolved

	ReadyExceptions    int64 // retryable, will be reclaimed
	InflightExceptions int64 // currently leased out to a retry attempt
	DeadExceptions     int64 // DLQ size

	OldestUnackedAge time.Duration // age of the oldest ready/inflight exception; 0 if none outstanding

	OpenLeases int64
}

// GroupLag is a group's drain progress -- the retire-relevant distillation
// of its snapshot.
type GroupLag struct {
	ConsumerGroup    string
	Committed        int64
	Head             int64
	Lag              int64 // Head - Committed, floored at 0
	ParkedExceptions int64 // delivery rows still 'ready' or 'inflight'
}

func (s *ConsumerGroupSnapshot) GroupLag() GroupLag {
	return GroupLag{
		ConsumerGroup:    s.ConsumerGroup,
		Committed:        s.Committed,
		Head:             s.Head,
		Lag:              max(s.Backlog, 0),
		ParkedExceptions: s.ReadyExceptions + s.InflightExceptions,
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
				WHERE consumer_group = $1 AND status = 'ready'
			), 0) AS ready_exceptions,
			COALESCE((
				SELECT COUNT(*)
				FROM %[2]s
				WHERE consumer_group = $1 AND status = 'inflight'
			), 0) AS inflight_exceptions,
			COALESCE((
				SELECT COUNT(*)
				FROM %[2]s
				WHERE consumer_group = $1 AND status = 'dead'
			), 0) AS dead_exceptions,
			(
				SELECT MIN(created_at)
				FROM %[2]s
				WHERE consumer_group = $1 AND status IN ('ready', 'inflight')
			) AS oldest_unacked_at,
			COALESCE((
				SELECT COUNT(*)
				FROM lease
				WHERE consumer_group = $1 AND topic_id = $2
			), 0) AS open_leases
		FROM cursor c
		WHERE c.consumer_group = $1 AND c.topic_id = $2;
	`, topic.MessageLogTable(topicID), topic.DeliveryTable(topicID))

	var s ConsumerGroupSnapshot
	var oldestUnackedAt *time.Time
	err := d.Datastore.Pool.QueryRow(ctx, sql, consumerGroup, topicID).Scan(
		&s.Claimed,
		&s.Committed,
		&s.Head,
		&s.ReadyExceptions,
		&s.InflightExceptions,
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

// ListConsumerGroups is every group with a cursor on topicID -- the groups a
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
	rows, err := d.Datastore.Pool.Query(ctx,
		`SELECT consumer_group FROM cursor WHERE topic_id = $1 ORDER BY consumer_group;`, topicID)
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
