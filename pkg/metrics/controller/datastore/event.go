package datastore

import (
	"context"

	iTopic "github.com/agentstax/vulkan/internal/topic"
	"github.com/agentstax/vulkan/pkg/metrics"
)

// EventTimestamps is every distinct (message, attempt) of eventType under
// routingKey on __system.metrics's own message log, with the earliest time
// each was seen.
func (d *MetricsDatastore) EventTimestamps(ctx context.Context, routingKey string, eventType metrics.EventType) ([]EventTimestampData, error) {
	var events []EventTimestampData
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		events, err = d.eventTimestamps(ctx, routingKey, eventType)
		return err
	})
	return events, err
}

func (d *MetricsDatastore) eventTimestamps(ctx context.Context, routingKey string, eventType metrics.EventType) ([]EventTimestampData, error) {
	metricsTopicId, err := d.resolveMetricsTopicId(ctx)
	if err != nil {
		return nil, err
	}

	sql := `
		SELECT
			(payload->>'message_id')::bigint,
			(payload->>'attempt')::int,
			MIN((payload->>'at')::timestamptz)
		FROM ` + iTopic.MessageLogTable(metricsTopicId) + `
		WHERE routing_key = $1 AND payload->>'type' = $2
		GROUP BY (payload->>'message_id')::bigint, (payload->>'attempt')::int;
	`
	rows, err := d.Datastore.Pool.Query(ctx, sql, routingKey, eventType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []EventTimestampData
	for rows.Next() {
		var data EventTimestampData
		if err := rows.Scan(&data.MessageId, &data.Attempt, &data.At); err != nil {
			return nil, err
		}
		events = append(events, data)
	}
	return events, rows.Err()
}

// resolveMetricsTopicId is the __system.metrics topic's own id.
func (d *MetricsDatastore) resolveMetricsTopicId(ctx context.Context) (int64, error) {
	var id int64
	err := d.Datastore.Pool.QueryRow(ctx,
		`SELECT id FROM topic WHERE name = $1 AND schema_version = 1;`, metrics.TopicName,
	).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}
