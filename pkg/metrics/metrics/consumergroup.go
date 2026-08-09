package metrics

import (
	"context"

	"github.com/agentstax/vulkan/pkg/metrics/controller"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// consumerGroupMetric owns the otel ObservableGauge instruments for one
// (group, topic)'s snapshot.
type consumerGroupMetric struct {
	controller *controller.MetricsController
	topicId    int64
	group      string

	cursorHead          metric.Int64ObservableGauge
	cursorClaimed       metric.Int64ObservableGauge
	cursorCommitted     metric.Int64ObservableGauge
	cursorBacklog       metric.Int64ObservableGauge
	cursorInflight      metric.Int64ObservableGauge
	readyExceptions     metric.Int64ObservableGauge
	inflightExceptions  metric.Int64ObservableGauge
	deferredExceptions  metric.Int64ObservableGauge
	deadExceptions      metric.Int64ObservableGauge
	oldestUnresolvedAge metric.Int64ObservableGauge
	openLeases          metric.Int64ObservableGauge

	abandonedOutstanding         metric.Int64ObservableGauge
	abandonedTotal               metric.Int64ObservableGauge
	abandonedSelfClearLatencyAvg metric.Int64ObservableGauge
	// group/topic identity, precomputed once so every observation reuses the
	// same option instead of rebuilding an attribute slice per callback.
	attrs metric.MeasurementOption
}

// RegisterConsumerGroupMetric registers one (group, topic)'s snapshot gauges
// against the meter. Call once per consumer instance, once its topic is
// resolved (topicId/topicName/topicVersion become known at Register) --
// calling it again for the same pair registers duplicate instruments.
func (m *Metrics) RegisterConsumerGroupMetric(group string, topicId int64, topicName string, topicVersion int64) error {
	cursorHead, err := m.meter.Int64ObservableGauge(
		"vulkan.consumer.cursor.head",
		metric.WithDescription("Highest message id ever appended to this topic's log -- the log frontier."),
		metric.WithUnit("{message}"),
	)
	if err != nil {
		return err
	}

	cursorClaimed, err := m.meter.Int64ObservableGauge(
		"vulkan.consumer.cursor.claimed",
		metric.WithDescription("cursor.claimed -- this group's read frontier."),
		metric.WithUnit("{message}"),
	)
	if err != nil {
		return err
	}

	cursorCommitted, err := m.meter.Int64ObservableGauge(
		"vulkan.consumer.cursor.committed",
		metric.WithDescription("cursor.committed -- everything at or below this id is done or dead."),
		metric.WithUnit("{message}"),
	)
	if err != nil {
		return err
	}

	cursorBacklog, err := m.meter.Int64ObservableGauge(
		"vulkan.consumer.cursor.backlog",
		metric.WithDescription("head - committed -- the waterline gap."),
		metric.WithUnit("{message}"),
	)
	if err != nil {
		return err
	}

	cursorInflight, err := m.meter.Int64ObservableGauge(
		"vulkan.consumer.cursor.inflight",
		metric.WithDescription("claimed - committed -- claimed but not yet resolved."),
		metric.WithUnit("{message}"),
	)
	if err != nil {
		return err
	}

	readyExceptions, err := m.meter.Int64ObservableGauge(
		"vulkan.consumer.exceptions.ready",
		metric.WithDescription("Delivery rows waiting to be retried."),
		metric.WithUnit("{exception}"),
	)
	if err != nil {
		return err
	}

	inflightExceptions, err := m.meter.Int64ObservableGauge(
		"vulkan.consumer.exceptions.inflight",
		metric.WithDescription("Delivery rows currently leased out to a retry attempt."),
		metric.WithUnit("{exception}"),
	)
	if err != nil {
		return err
	}

	deferredExceptions, err := m.meter.Int64ObservableGauge(
		"vulkan.consumer.exceptions.deferred",
		metric.WithDescription("Delivery rows waiting for their compaction key's key_lease to free."),
		metric.WithUnit("{exception}"),
	)
	if err != nil {
		return err
	}

	deadExceptions, err := m.meter.Int64ObservableGauge(
		"vulkan.consumer.exceptions.dead",
		metric.WithDescription("Dead-lettered delivery rows -- DLQ size."),
		metric.WithUnit("{exception}"),
	)
	if err != nil {
		return err
	}

	oldestUnresolvedAge, err := m.meter.Int64ObservableGauge(
		"vulkan.consumer.exceptions.oldest_unresolved_age",
		metric.WithDescription("Age of the oldest ready/inflight/deferred exception; 0 if none outstanding."),
		metric.WithUnit("ms"),
	)
	if err != nil {
		return err
	}

	openLeases, err := m.meter.Int64ObservableGauge(
		"vulkan.consumer.open_leases",
		metric.WithDescription("Currently open leases for this (group, topic)."),
		metric.WithUnit("{lease}"),
	)
	if err != nil {
		return err
	}

	abandonedOutstanding, err := m.meter.Int64ObservableGauge(
		"vulkan.consumer.abandoned_routines.outstanding",
		metric.WithDescription("Abandoned events with no matching cleared event -- routines still running past their timeout."),
		metric.WithUnit("{routine}"),
	)
	if err != nil {
		return err
	}

	abandonedTotal, err := m.meter.Int64ObservableGauge(
		"vulkan.consumer.abandoned_routines.total",
		metric.WithDescription("Distinct abandoned (message, attempt) events within the metrics topic's retention window."),
		metric.WithUnit("{routine}"),
	)
	if err != nil {
		return err
	}

	abandonedSelfClearLatencyAvg, err := m.meter.Int64ObservableGauge(
		"vulkan.consumer.abandoned_routines.self_clear_latency_avg",
		metric.WithDescription("Mean cleared - abandoned latency over matched pairs; 0 while no pair has cleared."),
		metric.WithUnit("ms"),
	)
	if err != nil {
		return err
	}

	c := &consumerGroupMetric{
		controller: m.controller,
		topicId:    topicId,
		group:      group,

		cursorHead:          cursorHead,
		cursorClaimed:       cursorClaimed,
		cursorCommitted:     cursorCommitted,
		cursorBacklog:       cursorBacklog,
		cursorInflight:      cursorInflight,
		readyExceptions:     readyExceptions,
		inflightExceptions:  inflightExceptions,
		deferredExceptions:  deferredExceptions,
		deadExceptions:      deadExceptions,
		oldestUnresolvedAge: oldestUnresolvedAge,
		openLeases:          openLeases,

		abandonedOutstanding:         abandonedOutstanding,
		abandonedTotal:               abandonedTotal,
		abandonedSelfClearLatencyAvg: abandonedSelfClearLatencyAvg,

		attrs: metric.WithAttributeSet(attribute.NewSet(
			attribute.String("messaging.consumer.group.name", group),
			attribute.String("messaging.destination.name", topicName),
			attribute.Int64("vulkan.topic.schema_version", topicVersion),
		)),
	}

	_, err = m.meter.RegisterCallback(c.observe,
		cursorHead,
		cursorClaimed,
		cursorCommitted,
		cursorBacklog,
		cursorInflight,
		readyExceptions,
		inflightExceptions,
		deferredExceptions,
		deadExceptions,
		oldestUnresolvedAge,
		openLeases,
		abandonedOutstanding,
		abandonedTotal,
		abandonedSelfClearLatencyAvg,
	)
	return err
}

// observe is the callback behind every gauge above -- one
// ConsumerGroupSnapshot call per collection cycle feeds all
// instruments, not one query per instrument.
func (c *consumerGroupMetric) observe(ctx context.Context, o metric.Observer) error {
	snapshot, err := c.controller.ConsumerGroupSnapshot(ctx, c.topicId, c.group)
	if err != nil {
		return err
	}

	o.ObserveInt64(c.cursorHead, snapshot.Cursor.Head, c.attrs)
	o.ObserveInt64(c.cursorClaimed, snapshot.Cursor.Claimed, c.attrs)
	o.ObserveInt64(c.cursorCommitted, snapshot.Cursor.Committed, c.attrs)
	o.ObserveInt64(c.cursorBacklog, snapshot.Cursor.Backlog, c.attrs)
	o.ObserveInt64(c.cursorInflight, snapshot.Cursor.Inflight, c.attrs)
	o.ObserveInt64(c.readyExceptions, snapshot.Exceptions.Ready, c.attrs)
	o.ObserveInt64(c.inflightExceptions, snapshot.Exceptions.Inflight, c.attrs)
	o.ObserveInt64(c.deferredExceptions, snapshot.Exceptions.Deferred, c.attrs)
	o.ObserveInt64(c.deadExceptions, snapshot.Exceptions.Dead, c.attrs)
	o.ObserveInt64(c.oldestUnresolvedAge, snapshot.Exceptions.OldestUnresolvedAge.Milliseconds(), c.attrs)
	o.ObserveInt64(c.openLeases, snapshot.OpenLeases, c.attrs)
	o.ObserveInt64(c.abandonedOutstanding, snapshot.AbandonedRoutines.Outstanding, c.attrs)
	o.ObserveInt64(c.abandonedTotal, snapshot.AbandonedRoutines.Total, c.attrs)
	o.ObserveInt64(c.abandonedSelfClearLatencyAvg, snapshot.AbandonedRoutines.SelfClearLatencyAvg.Milliseconds(), c.attrs)

	return nil
}
