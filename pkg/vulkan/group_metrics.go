package vulkan

import (
	"context"

	"github.com/agentstax/vulkan/pkg/common/diagnostic"
	"github.com/agentstax/vulkan/pkg/metrics"
)

// GroupMetricsHandle names one consumer group's metrics resource, holding no
// database row.
type GroupMetricsHandle struct {
	topicName string
	groupName string
	client    *Client
}

// Metrics returns the consumer group's metrics handle. It performs no I/O.
func (g *GroupHandle[Message]) Metrics() *GroupMetricsHandle {
	return &GroupMetricsHandle{topicName: g.topicName, groupName: g.name, client: g.client}
}

// Definitions returns the consumer-group-scoped Vulkan metric definitions
// ordered by VK code. It performs no I/O.
func (g *GroupMetricsHandle) Definitions() []MetricDefinition {
	return metrics.Definitions(diagnostic.MetricScopeConsumerGroup)
}

// Snapshot computes the consumer group's live metrics from its source tables.
func (g *GroupMetricsHandle) Snapshot(ctx context.Context) (*ConsumerGroupSnapshot, error) {
	return g.client.admin.GroupMetrics(ctx, g.topicName, g.groupName)
}

// CursorHead selects the group's topic-head series.
func (g *GroupMetricsHandle) CursorHead() *MetricHandle {
	return g.metric(metrics.MetricCursorHead)
}

// CursorClaimed selects the group's claimed-cursor series.
func (g *GroupMetricsHandle) CursorClaimed() *MetricHandle {
	return g.metric(metrics.MetricCursorClaimed)
}

// CursorCommitted selects the group's committed-cursor series.
func (g *GroupMetricsHandle) CursorCommitted() *MetricHandle {
	return g.metric(metrics.MetricCursorCommitted)
}

// CursorBacklog selects the group's cursor-backlog series.
func (g *GroupMetricsHandle) CursorBacklog() *MetricHandle {
	return g.metric(metrics.MetricCursorBacklog)
}

// CursorInflight selects the group's cursor-inflight series.
func (g *GroupMetricsHandle) CursorInflight() *MetricHandle {
	return g.metric(metrics.MetricCursorInflight)
}

// ReadyExceptions selects the group's ready-exception series.
func (g *GroupMetricsHandle) ReadyExceptions() *MetricHandle {
	return g.metric(metrics.MetricReadyExceptions)
}

// InflightExceptions selects the group's inflight-exception series.
func (g *GroupMetricsHandle) InflightExceptions() *MetricHandle {
	return g.metric(metrics.MetricInflightExceptions)
}

// DeferredExceptions selects the group's deferred-exception series.
func (g *GroupMetricsHandle) DeferredExceptions() *MetricHandle {
	return g.metric(metrics.MetricDeferredExceptions)
}

// DeadExceptions selects the group's dead-exception series.
func (g *GroupMetricsHandle) DeadExceptions() *MetricHandle {
	return g.metric(metrics.MetricDeadExceptions)
}

// OldestUnresolvedAge selects the group's oldest-unresolved-exception-age
// series.
func (g *GroupMetricsHandle) OldestUnresolvedAge() *MetricHandle {
	return g.metric(metrics.MetricOldestUnresolvedAge)
}

// OpenLeases selects the group's open-lease series.
func (g *GroupMetricsHandle) OpenLeases() *MetricHandle {
	return g.metric(metrics.MetricOpenLeases)
}

// AbandonedRoutinesOutstanding selects the group's outstanding-abandoned-
// routines series.
func (g *GroupMetricsHandle) AbandonedRoutinesOutstanding() *MetricHandle {
	return g.metric(metrics.MetricAbandonedOutstanding)
}

// AbandonedRoutinesTotal selects the group's total-abandoned-routines series.
func (g *GroupMetricsHandle) AbandonedRoutinesTotal() *MetricHandle {
	return g.metric(metrics.MetricAbandonedTotal)
}

// AbandonedRoutinesSelfClearLatencyAverage selects the group's average
// abandoned-routine self-clear-latency series.
func (g *GroupMetricsHandle) AbandonedRoutinesSelfClearLatencyAverage() *MetricHandle {
	return g.metric(metrics.MetricAbandonedSelfClearLatencyAvg)
}

func (g *GroupMetricsHandle) metric(declared *diagnostic.DiagnosticMetric) *MetricHandle {
	attributes := map[string]string{"topic": g.topicName, "group": g.groupName}
	return newMetricHandle(g.client, declared, declared.Name, attributes)
}
