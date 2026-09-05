package vulkan

import (
	"testing"

	"github.com/agentstax/vulkan/pkg/common/diagnostic"
	"github.com/agentstax/vulkan/pkg/metrics"
)

func TestMetricSelectorsCoverResourceScopedCatalog(t *testing.T) {
	client := &Client{}
	systemMetrics := client.System().Metrics()
	topicMetrics := client.Topic[RawPayload]("orders").Metrics()
	groupMetrics := client.Topic[RawPayload]("orders").Group("billing").Metrics()

	selectors := []struct {
		name       string
		handle     *MetricHandle
		declared   *diagnostic.DiagnosticMetric
		attributes map[string]string
	}{
		{"UnclaimedWorkers", systemMetrics.UnclaimedWorkers(), metrics.MetricUnclaimedWorkers, nil},
		{"OldestUnclaimedAge", systemMetrics.OldestUnclaimedAge(), metrics.MetricOldestUnclaimedAge, nil},
		{"FailingWorkers", systemMetrics.FailingWorkers(), metrics.MetricFailingWorkers, nil},
		{"OverdueSchedules", systemMetrics.OverdueSchedules(), metrics.MetricOverdueSchedules, nil},
		{"OldestDueAge", systemMetrics.OldestDueAge(), metrics.MetricOldestDueAge, nil},
		{"SuspendedSchedules", systemMetrics.SuspendedSchedules(), metrics.MetricSuspendedSchedules, nil},
		{"ActiveAlerts", systemMetrics.ActiveAlerts(), metrics.MetricActiveAlerts, nil},
		{"ResolvedAlerts", systemMetrics.ResolvedAlerts(), metrics.MetricResolvedAlerts, nil},
		{"CheckTopicsEvaluated", systemMetrics.CheckTopicsEvaluated("partition_count"), metrics.MetricCheckTopicsEvaluated, map[string]string{"alert": "partition_count"}},
		{"CheckTopicsFailed", systemMetrics.CheckTopicsFailed("partition_count"), metrics.MetricCheckTopicsFailed, map[string]string{"alert": "partition_count"}},
		{"CheckPublishedAlerts", systemMetrics.CheckPublishedAlerts("partition_count"), metrics.MetricCheckPublishedAlerts, map[string]string{"alert": "partition_count"}},
		{"CheckResolvedAlerts", systemMetrics.CheckResolvedAlerts("partition_count"), metrics.MetricCheckResolvedAlerts, map[string]string{"alert": "partition_count"}},
		{"Compacted", topicMetrics.Compacted(), metrics.MetricTopicCompacted, map[string]string{"topic": "orders"}},
		{"CursorHead", groupMetrics.CursorHead(), metrics.MetricCursorHead, map[string]string{"topic": "orders", "group": "billing"}},
		{"CursorClaimed", groupMetrics.CursorClaimed(), metrics.MetricCursorClaimed, map[string]string{"topic": "orders", "group": "billing"}},
		{"CursorCommitted", groupMetrics.CursorCommitted(), metrics.MetricCursorCommitted, map[string]string{"topic": "orders", "group": "billing"}},
		{"CursorBacklog", groupMetrics.CursorBacklog(), metrics.MetricCursorBacklog, map[string]string{"topic": "orders", "group": "billing"}},
		{"CursorInflight", groupMetrics.CursorInflight(), metrics.MetricCursorInflight, map[string]string{"topic": "orders", "group": "billing"}},
		{"ReadyExceptions", groupMetrics.ReadyExceptions(), metrics.MetricReadyExceptions, map[string]string{"topic": "orders", "group": "billing"}},
		{"InflightExceptions", groupMetrics.InflightExceptions(), metrics.MetricInflightExceptions, map[string]string{"topic": "orders", "group": "billing"}},
		{"DeferredExceptions", groupMetrics.DeferredExceptions(), metrics.MetricDeferredExceptions, map[string]string{"topic": "orders", "group": "billing"}},
		{"DeadExceptions", groupMetrics.DeadExceptions(), metrics.MetricDeadExceptions, map[string]string{"topic": "orders", "group": "billing"}},
		{"OldestUnresolvedAge", groupMetrics.OldestUnresolvedAge(), metrics.MetricOldestUnresolvedAge, map[string]string{"topic": "orders", "group": "billing"}},
		{"OpenLeases", groupMetrics.OpenLeases(), metrics.MetricOpenLeases, map[string]string{"topic": "orders", "group": "billing"}},
		{"AbandonedRoutinesOutstanding", groupMetrics.AbandonedRoutinesOutstanding(), metrics.MetricAbandonedOutstanding, map[string]string{"topic": "orders", "group": "billing"}},
		{"AbandonedRoutinesTotal", groupMetrics.AbandonedRoutinesTotal(), metrics.MetricAbandonedTotal, map[string]string{"topic": "orders", "group": "billing"}},
		{"AbandonedRoutinesSelfClearLatencyAverage", groupMetrics.AbandonedRoutinesSelfClearLatencyAverage(), metrics.MetricAbandonedSelfClearLatencyAvg, map[string]string{"topic": "orders", "group": "billing"}},
	}

	seen := make(map[*diagnostic.DiagnosticMetric]int, len(selectors))
	for _, selector := range selectors {
		if selector.handle.declared != selector.declared {
			t.Errorf("%s resolved %p, want %p", selector.name, selector.handle.declared, selector.declared)
		}
		wantedMessageKey := metrics.MeasurementKey(selector.declared.Name, selector.attributes)
		if selector.handle.messageKey != wantedMessageKey {
			t.Errorf("%s message key = %q, want %q", selector.name, selector.handle.messageKey, wantedMessageKey)
		}
		seen[selector.handle.declared]++
	}

	definitions := metrics.Definitions(
		diagnostic.MetricScopeSystem,
		diagnostic.MetricScopeTopic,
		diagnostic.MetricScopeConsumerGroup,
	)
	if len(selectors) != len(definitions) {
		t.Fatalf("%d selectors cover %d definitions", len(selectors), len(definitions))
	}
	for _, definition := range definitions {
		declared, found := diagnostic.GetMetric(definition.Name)
		if !found {
			t.Errorf("definition %q is not registered", definition.Name)
			continue
		}
		if seen[declared] != 1 {
			t.Errorf("definition %q appears through %d selectors, want 1", definition.Name, seen[declared])
		}
	}
}

func TestMetricHandleConstructorsPerformNoIO(t *testing.T) {
	client := &Client{}

	systemMetrics := client.System().Metrics()
	if len(systemMetrics.Definitions()) != len(metrics.Definitions()) {
		t.Fatal("system definitions do not expose the complete catalog")
	}
	if len(client.Topic[RawPayload]("orders").Metrics().Definitions()) != 1 {
		t.Fatal("topic definitions do not expose the topic catalog")
	}
	if len(client.Topic[RawPayload]("orders").Group("billing").Metrics().Definitions()) != 14 {
		t.Fatal("group definitions do not expose the consumer-group catalog")
	}

	known := systemMetrics.Metric(metrics.MetricCursorBacklog.Name, map[string]string{"topic": "orders", "group": "billing"})
	if known.declared != metrics.MetricCursorBacklog {
		t.Fatal("arbitrary selector did not bind its registered definition")
	}
	custom := systemMetrics.Metric("checkout.request.duration", map[string]string{"region": "us-east-1"})
	if custom.declared != nil {
		t.Fatal("user metric unexpectedly bound a Vulkan definition")
	}
}
