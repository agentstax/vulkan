package metrics

import (
	"context"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/internal/topic"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// QueueStateSnapshot is the live, DB-truth picture of one (group, topic)'s
// queue -- answers "what's true right now" for state that multiple consumer
// processes share (cursor/delivery/lease), which no in-process counter can.
type QueueStateSnapshot struct {
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

// QueueState owns the otel ObservableGauge instruments for one (group, topic)
type QueueState struct {
	datastore *consumerMetricsDatastore
	topicID   int64
	group     string

	// otel instruments
	head               metric.Int64ObservableGauge
	claimed            metric.Int64ObservableGauge
	committed          metric.Int64ObservableGauge
	backlog            metric.Int64ObservableGauge
	inflight           metric.Int64ObservableGauge
	readyExceptions    metric.Int64ObservableGauge
	inflightExceptions metric.Int64ObservableGauge
	deadExceptions     metric.Int64ObservableGauge
	oldestUnackedAge   metric.Int64ObservableGauge
	openLeases         metric.Int64ObservableGauge
	// group/topic identity, precomputed once so every observation reuses the
	// same option instead of rebuilding an attribute slice per callback.
	attrs metric.MeasurementOption
}

func NewQueueState(meter metric.Meter, group string, topicID int64, topicName string, ds *consumerMetricsDatastore) (*QueueState, error) {
	head, err := meter.Int64ObservableGauge(
		"vulkan.consumer.queue_state.head",
		metric.WithDescription("Highest message id ever appended to this topic's log -- the log frontier."),
		metric.WithUnit("{message}"),
	)
	if err != nil {
		return nil, err
	}

	claimed, err := meter.Int64ObservableGauge(
		"vulkan.consumer.queue_state.claimed",
		metric.WithDescription("cursor.claimed -- this group's read frontier."),
		metric.WithUnit("{message}"),
	)
	if err != nil {
		return nil, err
	}

	committed, err := meter.Int64ObservableGauge(
		"vulkan.consumer.queue_state.committed",
		metric.WithDescription("cursor.committed -- everything at or below this id is done or dead."),
		metric.WithUnit("{message}"),
	)
	if err != nil {
		return nil, err
	}

	backlog, err := meter.Int64ObservableGauge(
		"vulkan.consumer.queue_state.backlog",
		metric.WithDescription("head - committed -- the waterline gap."),
		metric.WithUnit("{message}"),
	)
	if err != nil {
		return nil, err
	}

	inflight, err := meter.Int64ObservableGauge(
		"vulkan.consumer.queue_state.inflight",
		metric.WithDescription("claimed - committed -- claimed but not yet resolved."),
		metric.WithUnit("{message}"),
	)
	if err != nil {
		return nil, err
	}

	readyExceptions, err := meter.Int64ObservableGauge(
		"vulkan.consumer.queue_state.ready_exceptions",
		metric.WithDescription("Parked delivery rows waiting to be retried."),
		metric.WithUnit("{exception}"),
	)
	if err != nil {
		return nil, err
	}

	inflightExceptions, err := meter.Int64ObservableGauge(
		"vulkan.consumer.queue_state.inflight_exceptions",
		metric.WithDescription("Parked delivery rows currently leased out to a retry attempt."),
		metric.WithUnit("{exception}"),
	)
	if err != nil {
		return nil, err
	}

	deadExceptions, err := meter.Int64ObservableGauge(
		"vulkan.consumer.queue_state.dead_exceptions",
		metric.WithDescription("Dead-lettered delivery rows -- DLQ size."),
		metric.WithUnit("{exception}"),
	)
	if err != nil {
		return nil, err
	}

	oldestUnackedAge, err := meter.Int64ObservableGauge(
		"vulkan.consumer.queue_state.oldest_unacked_age",
		metric.WithDescription("Age of the oldest ready/inflight exception; 0 if none outstanding."),
		metric.WithUnit("ms"),
	)
	if err != nil {
		return nil, err
	}

	openLeases, err := meter.Int64ObservableGauge(
		"vulkan.consumer.queue_state.open_leases",
		metric.WithDescription("Currently open leases for this (group, topic)."),
		metric.WithUnit("{lease}"),
	)
	if err != nil {
		return nil, err
	}

	q := &QueueState{
		datastore: ds,
		topicID:   topicID,
		group:     group,

		head:               head,
		claimed:            claimed,
		committed:          committed,
		backlog:            backlog,
		inflight:           inflight,
		readyExceptions:    readyExceptions,
		inflightExceptions: inflightExceptions,
		deadExceptions:     deadExceptions,
		oldestUnackedAge:   oldestUnackedAge,
		openLeases:         openLeases,

		attrs: metric.WithAttributeSet(attribute.NewSet(
			attribute.String("messaging.consumer.group.name", group),
			attribute.String("messaging.destination.name", topicName),
		)),
	}

	if _, err := meter.RegisterCallback(q.observe,
		head,
		claimed,
		committed,
		backlog,
		inflight,
		readyExceptions,
		inflightExceptions,
		deadExceptions,
		oldestUnackedAge,
		openLeases,
	); err != nil {
		return nil, err
	}

	return q, nil
}

// observe is the callback behind every gauge above -- one Snapshot call per
// collection cycle feeds all ten instruments, not one query per instrument.
func (q *QueueState) observe(ctx context.Context, o metric.Observer) error {
	snapshot, err := q.Snapshot(ctx)
	if err != nil {
		return err
	}

	o.ObserveInt64(q.head, snapshot.Head, q.attrs)
	o.ObserveInt64(q.claimed, snapshot.Claimed, q.attrs)
	o.ObserveInt64(q.committed, snapshot.Committed, q.attrs)
	o.ObserveInt64(q.backlog, snapshot.Backlog, q.attrs)
	o.ObserveInt64(q.inflight, snapshot.Inflight, q.attrs)
	o.ObserveInt64(q.readyExceptions, snapshot.ReadyExceptions, q.attrs)
	o.ObserveInt64(q.inflightExceptions, snapshot.InflightExceptions, q.attrs)
	o.ObserveInt64(q.deadExceptions, snapshot.DeadExceptions, q.attrs)
	o.ObserveInt64(q.oldestUnackedAge, snapshot.OldestUnackedAge.Milliseconds(), q.attrs)
	o.ObserveInt64(q.openLeases, snapshot.OpenLeases, q.attrs)

	return nil
}

// Snapshot is the current QueueStateSnapshot, queried live from Postgres --
// works with no otel exporter/backend attached, same data observe reports.
func (q *QueueState) Snapshot(ctx context.Context) (*QueueStateSnapshot, error) {
	var snapshot *QueueStateSnapshot
	err := q.datastore.Retry.Wrap(ctx, func() error {
		var err error
		snapshot, err = q.datastore.queueStateSnapshot(ctx, q.topicID, q.group)
		return err
	})
	return snapshot, err
}

func (d *consumerMetricsDatastore) queueStateSnapshot(ctx context.Context, topicID int64, consumerGroup string) (*QueueStateSnapshot, error) {
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

	var s QueueStateSnapshot
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
