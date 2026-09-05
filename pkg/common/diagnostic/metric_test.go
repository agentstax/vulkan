package diagnostic

import "testing"

var metricTestBacklog = NewDiagnosticMetric(
	"VK9920",
	"vulkan.test.cursor.backlog",
	"gauge",
	"{message}",
	"test messages beyond the committed cursor",
	MetricScopeConsumerGroup,
	"topic",
	"group",
)

func TestMetricCarriesMetadata(t *testing.T) {
	if metricTestBacklog.Scope != MetricScopeConsumerGroup {
		t.Fatalf("scope = %q", metricTestBacklog.Scope)
	}
	if len(metricTestBacklog.AttributeKeys) != 2 || metricTestBacklog.AttributeKeys[0] != "topic" || metricTestBacklog.AttributeKeys[1] != "group" {
		t.Fatalf("attribute keys = %v", metricTestBacklog.AttributeKeys)
	}
}

func TestNewMetricCopiesAttributeKeys(t *testing.T) {
	attributeKeys := []string{"topic"}
	declared := NewDiagnosticMetric(
		"VK9921",
		"vulkan.test.topic.depth",
		"gauge",
		"{message}",
		"test messages retained for a topic",
		MetricScopeTopic,
		attributeKeys...,
	)
	attributeKeys[0] = "changed"

	if declared.AttributeKeys[0] != "topic" {
		t.Fatalf("constructor retained the caller's slice: %v", declared.AttributeKeys)
	}
}

func TestNewMetricRejectsInvalidMetadata(t *testing.T) {
	tests := []struct {
		name          string
		code          string
		metricName    string
		kind          string
		description   string
		scope         MetricScope
		attributeKeys []string
	}{
		{name: "empty name", code: "VK9922", kind: "gauge", description: "test depth", scope: MetricScopeSystem},
		{name: "empty kind", code: "VK9923", metricName: "vulkan.test.empty_kind", description: "test depth", scope: MetricScopeSystem},
		{name: "empty description", code: "VK9924", metricName: "vulkan.test.empty_description", kind: "gauge", scope: MetricScopeSystem},
		{name: "empty scope", code: "VK9925", metricName: "vulkan.test.empty_scope", kind: "gauge", description: "test depth"},
		{name: "unknown scope", code: "VK9926", metricName: "vulkan.test.unknown_scope", kind: "gauge", description: "test depth", scope: MetricScope("worker")},
		{name: "empty attribute key", code: "VK9927", metricName: "vulkan.test.empty_attribute", kind: "gauge", description: "test depth", scope: MetricScopeSystem, attributeKeys: []string{""}},
		{name: "duplicate attribute key", code: "VK9928", metricName: "vulkan.test.duplicate_attribute", kind: "gauge", description: "test depth", scope: MetricScopeSystem, attributeKeys: []string{"topic", "topic"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			expectPanic(t, func() {
				NewDiagnosticMetric(test.code, test.metricName, test.kind, "", test.description, test.scope, test.attributeKeys...)
			})
		})
	}
}

func TestMetricsListsOrderedByCode(t *testing.T) {
	listed := Metrics()
	for i := 1; i < len(listed); i++ {
		if listed[i-1].Code >= listed[i].Code {
			t.Fatalf("codes out of order: %s before %s", listed[i-1].Code, listed[i].Code)
		}
	}
}
