package vulkan

import (
	"context"

	"github.com/agentstax/vulkan/pkg/common/diagnostic"
	"github.com/agentstax/vulkan/pkg/metrics"
)

// SystemMetricsHandle names the installation-wide metrics resource, holding
// no database row.
type SystemMetricsHandle struct {
	client *Client
}

// Metrics returns the installation-wide metrics handle. It performs no I/O.
func (s *SystemHandle) Metrics() *SystemMetricsHandle {
	return &SystemMetricsHandle{client: s.client}
}

// Definitions returns every Vulkan built-in metric definition ordered by VK
// code. It performs no I/O.
func (s *SystemMetricsHandle) Definitions() []MetricDefinition {
	return metrics.Definitions()
}

// Latest returns the newest retained measurement for every published series,
// ordered by measurement key.
func (s *SystemMetricsHandle) Latest(ctx context.Context) ([]*Measurement, error) {
	stored, err := s.client.admin.ListMeasurements(ctx)
	if err != nil {
		return nil, err
	}
	return unwrapMeasurements(stored), nil
}

// Metric names one exact series by its wire name and complete attribute set.
// It performs no I/O. Built-in selectors are preferred when one is available.
func (s *SystemMetricsHandle) Metric(name string, attributes map[string]string) *MetricHandle {
	declared, _ := diagnostic.GetMetric(name)
	return newMetricHandle(s.client, declared, name, attributes)
}

// UnclaimedWorkers selects the system's unclaimed-worker series.
func (s *SystemMetricsHandle) UnclaimedWorkers() *MetricHandle {
	return s.metric(metrics.MetricUnclaimedWorkers, nil)
}

// OldestUnclaimedAge selects the system's oldest-unclaimed-worker-age series.
func (s *SystemMetricsHandle) OldestUnclaimedAge() *MetricHandle {
	return s.metric(metrics.MetricOldestUnclaimedAge, nil)
}

// FailingWorkers selects the system's failing-worker series.
func (s *SystemMetricsHandle) FailingWorkers() *MetricHandle {
	return s.metric(metrics.MetricFailingWorkers, nil)
}

// OverdueSchedules selects the system's overdue-schedule series.
func (s *SystemMetricsHandle) OverdueSchedules() *MetricHandle {
	return s.metric(metrics.MetricOverdueSchedules, nil)
}

// OldestDueAge selects the system's oldest-due-schedule-age series.
func (s *SystemMetricsHandle) OldestDueAge() *MetricHandle {
	return s.metric(metrics.MetricOldestDueAge, nil)
}

// SuspendedSchedules selects the system's suspended-schedule series.
func (s *SystemMetricsHandle) SuspendedSchedules() *MetricHandle {
	return s.metric(metrics.MetricSuspendedSchedules, nil)
}

// ActiveAlerts selects the system's active-alert series.
func (s *SystemMetricsHandle) ActiveAlerts() *MetricHandle {
	return s.metric(metrics.MetricActiveAlerts, nil)
}

// ResolvedAlerts selects the system's resolved-alert series.
func (s *SystemMetricsHandle) ResolvedAlerts() *MetricHandle {
	return s.metric(metrics.MetricResolvedAlerts, nil)
}

// CheckTopicsEvaluated selects the topics-evaluated series for one alert
// check.
func (s *SystemMetricsHandle) CheckTopicsEvaluated(alert string) *MetricHandle {
	attributes := map[string]string{"alert": alert}
	return s.metric(metrics.MetricCheckTopicsEvaluated, attributes)
}

// CheckTopicsFailed selects the topics-failed series for one alert check.
func (s *SystemMetricsHandle) CheckTopicsFailed(alert string) *MetricHandle {
	attributes := map[string]string{"alert": alert}
	return s.metric(metrics.MetricCheckTopicsFailed, attributes)
}

// CheckPublishedAlerts selects the published-alerts series for one alert
// check.
func (s *SystemMetricsHandle) CheckPublishedAlerts(alert string) *MetricHandle {
	attributes := map[string]string{"alert": alert}
	return s.metric(metrics.MetricCheckPublishedAlerts, attributes)
}

// CheckResolvedAlerts selects the resolved-alerts series for one alert check.
func (s *SystemMetricsHandle) CheckResolvedAlerts(alert string) *MetricHandle {
	attributes := map[string]string{"alert": alert}
	return s.metric(metrics.MetricCheckResolvedAlerts, attributes)
}

func (s *SystemMetricsHandle) metric(declared *diagnostic.DiagnosticMetric, attributes map[string]string) *MetricHandle {
	return newMetricHandle(s.client, declared, declared.Name, attributes)
}
