package monitor

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// consumerGroupGauges owns the otel ObservableGauge instruments for one
// (group, topic)'s queue state.
type consumerGroupGauges struct {
	monitor *Monitor
	topicID int64
	group   string

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

// RegisterConsumerGroup registers the queue-state gauges for one (group,
// topic) against the monitor's meter. Call once per consumer instance, once
// its topic is resolved (topicID/topicName/topicVersion become known at
// Register) -- calling it again for the same pair registers duplicate
// instruments.
func (m *Monitor) RegisterConsumerGroup(group string, topicID int64, topicName string, topicVersion int64) error {
	head, err := m.meter.Int64ObservableGauge(
		"vulkan.consumer.queue_state.head",
		metric.WithDescription("Highest message id ever appended to this topic's log -- the log frontier."),
		metric.WithUnit("{message}"),
	)
	if err != nil {
		return err
	}

	claimed, err := m.meter.Int64ObservableGauge(
		"vulkan.consumer.queue_state.claimed",
		metric.WithDescription("cursor.claimed -- this group's read frontier."),
		metric.WithUnit("{message}"),
	)
	if err != nil {
		return err
	}

	committed, err := m.meter.Int64ObservableGauge(
		"vulkan.consumer.queue_state.committed",
		metric.WithDescription("cursor.committed -- everything at or below this id is done or dead."),
		metric.WithUnit("{message}"),
	)
	if err != nil {
		return err
	}

	backlog, err := m.meter.Int64ObservableGauge(
		"vulkan.consumer.queue_state.backlog",
		metric.WithDescription("head - committed -- the waterline gap."),
		metric.WithUnit("{message}"),
	)
	if err != nil {
		return err
	}

	inflight, err := m.meter.Int64ObservableGauge(
		"vulkan.consumer.queue_state.inflight",
		metric.WithDescription("claimed - committed -- claimed but not yet resolved."),
		metric.WithUnit("{message}"),
	)
	if err != nil {
		return err
	}

	readyExceptions, err := m.meter.Int64ObservableGauge(
		"vulkan.consumer.queue_state.ready_exceptions",
		metric.WithDescription("Parked delivery rows waiting to be retried."),
		metric.WithUnit("{exception}"),
	)
	if err != nil {
		return err
	}

	inflightExceptions, err := m.meter.Int64ObservableGauge(
		"vulkan.consumer.queue_state.inflight_exceptions",
		metric.WithDescription("Parked delivery rows currently leased out to a retry attempt."),
		metric.WithUnit("{exception}"),
	)
	if err != nil {
		return err
	}

	deadExceptions, err := m.meter.Int64ObservableGauge(
		"vulkan.consumer.queue_state.dead_exceptions",
		metric.WithDescription("Dead-lettered delivery rows -- DLQ size."),
		metric.WithUnit("{exception}"),
	)
	if err != nil {
		return err
	}

	oldestUnackedAge, err := m.meter.Int64ObservableGauge(
		"vulkan.consumer.queue_state.oldest_unacked_age",
		metric.WithDescription("Age of the oldest ready/inflight exception; 0 if none outstanding."),
		metric.WithUnit("ms"),
	)
	if err != nil {
		return err
	}

	openLeases, err := m.meter.Int64ObservableGauge(
		"vulkan.consumer.queue_state.open_leases",
		metric.WithDescription("Currently open leases for this (group, topic)."),
		metric.WithUnit("{lease}"),
	)
	if err != nil {
		return err
	}

	g := &consumerGroupGauges{
		monitor: m,
		topicID: topicID,
		group:   group,

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
			attribute.Int64("vulkan.topic.schema_version", topicVersion),
		)),
	}

	_, err = m.meter.RegisterCallback(g.observe,
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
	)
	return err
}

// observe is the callback behind every gauge above -- one
// ConsumerGroupSnapshot call per collection cycle feeds all ten instruments,
// not one query per instrument.
func (g *consumerGroupGauges) observe(ctx context.Context, o metric.Observer) error {
	snapshot, err := g.monitor.Datastore.ConsumerGroupSnapshot(ctx, g.topicID, g.group)
	if err != nil {
		return err
	}

	o.ObserveInt64(g.head, snapshot.Head, g.attrs)
	o.ObserveInt64(g.claimed, snapshot.Claimed, g.attrs)
	o.ObserveInt64(g.committed, snapshot.Committed, g.attrs)
	o.ObserveInt64(g.backlog, snapshot.Backlog, g.attrs)
	o.ObserveInt64(g.inflight, snapshot.Inflight, g.attrs)
	o.ObserveInt64(g.readyExceptions, snapshot.ReadyExceptions, g.attrs)
	o.ObserveInt64(g.inflightExceptions, snapshot.InflightExceptions, g.attrs)
	o.ObserveInt64(g.deadExceptions, snapshot.DeadExceptions, g.attrs)
	o.ObserveInt64(g.oldestUnackedAge, snapshot.OldestUnackedAge.Milliseconds(), g.attrs)
	o.ObserveInt64(g.openLeases, snapshot.OpenLeases, g.attrs)

	return nil
}
