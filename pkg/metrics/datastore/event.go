package datastore

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/agentstax/vulkan/internal/topic"
	"github.com/agentstax/vulkan/pkg/metrics"
)

// EventSnapshot is derived from the __system.metrics event stream for one
// (topic, group) -- no in-process counter is kept anywhere, every number
// here comes from pairing abandoned/cleared events already on the topic.
type EventSnapshot struct {
	Outstanding         int64         // abandoned events with no matching cleared
	Total               int64         // distinct abandoned keys currently in the window
	SelfClearLatencyAvg time.Duration // mean(cleared.At - abandoned.At) over matched pairs; 0 if no pair has cleared yet
}

// EventSnapshot reads and pairs the abandoned/cleared events for (topicID,
// group) directly off __system.metrics's own message log -- no
// MessageConsumer needed. The window is whatever the metrics topic's own
// retention has physically kept; events past retention are already gone by
// the time this query runs, so nothing here needs its own time filter.
func (d *MetricsDatastore) EventSnapshot(ctx context.Context, topicID int64, group string) (*EventSnapshot, error) {
	var snapshot *EventSnapshot
	err := d.Retry.Wrap(ctx, func() error {
		var err error
		snapshot, err = d.eventSnapshot(ctx, topicID, group)
		return err
	})
	return snapshot, err
}

// abandonedKey is the (message, attempt) identity an abandoned event and its
// matching cleared event share -- topicID/group are already fixed by the
// routing key both queries filter on, so they're not part of the key.
type abandonedKey struct {
	MessageID int64
	Attempt   int
}

func (d *MetricsDatastore) eventSnapshot(ctx context.Context, topicID int64, group string) (*EventSnapshot, error) {
	metricsTopicID, err := d.resolveMetricsTopicID(ctx)
	if err != nil {
		return nil, err
	}

	routingKey := metrics.AbandonedRoutineKey(topicID, group)

	abandoned, err := d.eventTimestamps(ctx, metricsTopicID, routingKey, "abandoned")
	if err != nil {
		return nil, err
	}
	cleared, err := d.eventTimestamps(ctx, metricsTopicID, routingKey, "cleared")
	if err != nil {
		return nil, err
	}

	var s EventSnapshot
	var latencySum time.Duration
	var matched int64
	for key, abandonedAt := range abandoned {
		s.Total++
		clearedAt, ok := cleared[key]
		if !ok {
			s.Outstanding++
			continue
		}
		latencySum += clearedAt.Sub(abandonedAt)
		matched++
	}
	if matched > 0 {
		s.SelfClearLatencyAvg = latencySum / time.Duration(matched)
	}

	return &s, nil
}

// eventTimestamps is every distinct (message, attempt) of eventType under
// routingKey, earliest-first -- one query per event type keeps each query's
// intent obvious instead of one query encoding both via CASE/HAVING.
func (d *MetricsDatastore) eventTimestamps(ctx context.Context, metricsTopicID int64, routingKey, eventType string) (map[abandonedKey]time.Time, error) {
	sql := `
		SELECT
			(payload->>'message_id')::bigint,
			(payload->>'attempt')::int,
			MIN((payload->>'at')::timestamptz)
		FROM ` + topic.MessageLogTable(metricsTopicID) + `
		WHERE routing_key = $1 AND payload->>'type' = $2
		GROUP BY (payload->>'message_id')::bigint, (payload->>'attempt')::int;
	`

	rows, err := d.Datastore.Pool.Query(ctx, sql, routingKey, eventType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := make(map[abandonedKey]time.Time)
	for rows.Next() {
		var key abandonedKey
		var at time.Time
		if err := rows.Scan(&key.MessageID, &key.Attempt, &at); err != nil {
			return nil, err
		}
		events[key] = at
	}
	return events, rows.Err()
}

// resolveMetricsTopicID is the __system.metrics topic's own id, cached after
// the first lookup -- the id is assigned once at topic creation and never
// changes, so every later EventSnapshot call reuses it instead of re-querying.
func (d *MetricsDatastore) resolveMetricsTopicID(ctx context.Context) (int64, error) {
	if id := atomic.LoadInt64(&d.metricsTopicID); id != -1 {
		return id, nil
	}

	var id int64
	err := d.Datastore.Pool.QueryRow(ctx,
		`SELECT id FROM topic WHERE name = $1 ORDER BY schema_version DESC LIMIT 1;`, metrics.TopicName,
	).Scan(&id)
	if err != nil {
		return 0, err
	}

	atomic.StoreInt64(&d.metricsTopicID, id)
	return id, nil
}
