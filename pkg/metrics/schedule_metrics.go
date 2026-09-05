package metrics

import "github.com/agentstax/vulkan/pkg/common/diagnostic"

var MetricOverdueSchedules = diagnostic.NewDiagnosticMetric(
	"VK0070",
	"vulkan.schedule.state.overdue",
	string(MetricKindGauge),
	string(MetricUnitCount("found")),
	"unsuspended schedules due past the overdue threshold",
	diagnostic.MetricScopeSystem,
)

var MetricOldestDueAge = diagnostic.NewDiagnosticMetric(
	"VK0071",
	"vulkan.schedule.state.oldest_due_age",
	string(MetricKindGauge),
	string(MetricUnitMilliseconds),
	"largest time past next scheduled production among unsuspended schedules",
	diagnostic.MetricScopeSystem,
)

var MetricSuspendedSchedules = diagnostic.NewDiagnosticMetric(
	"VK0072",
	"vulkan.schedule.state.suspended",
	string(MetricKindGauge),
	string(MetricUnitCount("found")),
	"schedules excluded from overdue counts because they are suspended",
	diagnostic.MetricScopeSystem,
)
