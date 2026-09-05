package metrics

import "github.com/agentstax/vulkan/pkg/common/diagnostic"

var MetricActiveAlerts = diagnostic.NewDiagnosticMetric(
	"VK0073",
	"vulkan.alert.state.active_alerts",
	string(MetricKindGauge),
	string(MetricUnitCount("alert")),
	"retained alert heads whose status is active",
	diagnostic.MetricScopeSystem,
)

var MetricResolvedAlerts = diagnostic.NewDiagnosticMetric(
	"VK0074",
	"vulkan.alert.state.resolved_alerts",
	string(MetricKindGauge),
	string(MetricUnitCount("alert")),
	"retained alert heads whose status is resolved",
	diagnostic.MetricScopeSystem,
)

var MetricCheckTopicsEvaluated = diagnostic.NewDiagnosticMetric(
	"VK0075",
	"vulkan.alert.check.topics_evaluated",
	string(MetricKindGauge),
	string(MetricUnitCount("topic")),
	"topics the alert check evaluated",
	diagnostic.MetricScopeSystem,
	"alert",
)

var MetricCheckTopicsFailed = diagnostic.NewDiagnosticMetric(
	"VK0076",
	"vulkan.alert.check.topics_failed",
	string(MetricKindGauge),
	string(MetricUnitCount("topic")),
	"topics whose alert evaluation or result production did not complete",
	diagnostic.MetricScopeSystem,
	"alert",
)

var MetricCheckPublishedAlerts = diagnostic.NewDiagnosticMetric(
	"VK0077",
	"vulkan.alert.check.published_alerts",
	string(MetricKindGauge),
	string(MetricUnitCount("alert")),
	"alerts the check changed to active",
	diagnostic.MetricScopeSystem,
	"alert",
)

var MetricCheckResolvedAlerts = diagnostic.NewDiagnosticMetric(
	"VK0078",
	"vulkan.alert.check.resolved_alerts",
	string(MetricKindGauge),
	string(MetricUnitCount("alert")),
	"alerts the check changed to resolved",
	diagnostic.MetricScopeSystem,
	"alert",
)
