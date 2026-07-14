package metrics

import (
	"context"
	"fmt"
	"time"
)

// QueueState is the live, DB-truth picture of one (group, topic)'s queue --
// answers "what's true right now" for state that multiple consumer processes
// share (cursors/deliveries/leases), which no in-process counter can.
type QueueState struct {
	Head      int64 // highest message id ever appended -- the log frontier
	Claimed   int64 // cursors.claimed -- the read frontier
	Committed int64 // cursors.committed -- everything <= this is done/dead

	Backlog  int64 // Head - Committed -- the waterline gap
	Inflight int64 // Claimed - Committed -- claimed but not yet resolved

	ReadyExceptions    int64 // retryable, will be reclaimed
	InflightExceptions int64 // currently leased out to a retry attempt
	DeadExceptions     int64 // DLQ size

	OldestUnackedAge time.Duration // age of the oldest ready/inflight exception; 0 if none outstanding

	OpenLeases int64
}

// QueueState computes QueueState live from Postgres for one (group, topic).
func (d *consumerMetricsDatastore) QueueState(ctx context.Context, topicID int64, consumerGroup string) (*QueueState, error) {
	var state *QueueState
	err := d.Retry.Wrap(ctx, func() error {
		var err error
		state, err = d.queueState(ctx, topicID, consumerGroup)
		return err
	})
	return state, err
}

func (d *consumerMetricsDatastore) queueState(ctx context.Context, topicID int64, consumerGroup string) (*QueueState, error) {
	sql := fmt.Sprintf(`
		SELECT
			c.claimed,
			c.committed,
			COALESCE((
				SELECT MAX(id)
				FROM %s
			), 0) AS head,
			COALESCE((
				SELECT COUNT(*)
				FROM deliveries
				WHERE consumer_group = $1 AND topic_id = $2 AND status = 'ready'
			), 0) AS ready_exceptions,
			COALESCE((
				SELECT COUNT(*)
				FROM deliveries
				WHERE consumer_group = $1 AND topic_id = $2 AND status = 'inflight'
			), 0) AS inflight_exceptions,
			COALESCE((
				SELECT COUNT(*)
				FROM deliveries
				WHERE consumer_group = $1 AND topic_id = $2 AND status = 'dead'
			), 0) AS dead_exceptions,
			(
				SELECT MIN(created_at)
				FROM deliveries
				WHERE consumer_group = $1 AND topic_id = $2 AND status IN ('ready', 'inflight')
			) AS oldest_unacked_at,
			COALESCE((
				SELECT COUNT(*)
				FROM leases
				WHERE consumer_group = $1 AND topic_id = $2
			), 0) AS open_leases
		FROM cursors c
		WHERE c.consumer_group = $1 AND c.topic_id = $2;
	`, logTable(topicID))

	var s QueueState
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

	s.Backlog = s.Head - s.Committed
	s.Inflight = s.Claimed - s.Committed
	if oldestUnackedAt != nil {
		s.OldestUnackedAge = time.Since(*oldestUnackedAt)
	}

	return &s, nil
}

// logTable is topicID's own physical message log -- duplicated from
// pkg/consumer/datastore.go's unexported helper of the same name rather than
// shared, same as this whole package's postgres access.
func logTable(topicID int64) string {
	return fmt.Sprintf("message_log_%d", topicID)
}
