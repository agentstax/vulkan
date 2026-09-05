package vulkan

import (
	"context"

	"github.com/agentstax/vulkan/pkg/common/diagnostic"
	"github.com/agentstax/vulkan/pkg/metrics"
)

// TopicMetricsHandle names one topic's metrics resource, holding no database
// row.
type TopicMetricsHandle struct {
	topicName string
	client    *Client
}

// Metrics returns the topic's metrics handle. It performs no I/O.
func (t *TopicHandle[Message]) Metrics() *TopicMetricsHandle {
	return &TopicMetricsHandle{topicName: t.name, client: t.client}
}

// Definitions returns the topic-scoped Vulkan metric definitions ordered by VK
// code. It performs no I/O.
func (t *TopicMetricsHandle) Definitions() []MetricDefinition {
	return metrics.Definitions(diagnostic.MetricScopeTopic)
}

// Snapshot computes the topic's live metrics from its source tables.
func (t *TopicMetricsHandle) Snapshot(ctx context.Context) (*TopicSnapshot, error) {
	return t.client.admin.TopicMetrics(ctx, t.topicName)
}

// Compacted selects the topic's compacted-state series.
func (t *TopicMetricsHandle) Compacted() *MetricHandle {
	return t.metric(metrics.MetricTopicCompacted)
}

func (t *TopicMetricsHandle) metric(declared *diagnostic.DiagnosticMetric) *MetricHandle {
	attributes := map[string]string{"topic": t.topicName}
	return newMetricHandle(t.client, declared, declared.Name, attributes)
}
