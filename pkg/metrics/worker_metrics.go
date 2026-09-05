package metrics

import "github.com/agentstax/vulkan/pkg/common/diagnostic"

var MetricUnclaimedWorkers = diagnostic.NewDiagnosticMetric(
	"VK0067",
	"vulkan.worker.state.unclaimed_workers",
	string(MetricKindGauge),
	string(MetricUnitCount("worker")),
	"workers with no live instance and a nonzero target",
	diagnostic.MetricScopeSystem,
)

var MetricOldestUnclaimedAge = diagnostic.NewDiagnosticMetric(
	"VK0068",
	"vulkan.worker.state.oldest_unclaimed_age",
	string(MetricKindGauge),
	string(MetricUnitMilliseconds),
	"largest time since expiry among workers with no live instance and a nonzero target",
	diagnostic.MetricScopeSystem,
)

var MetricFailingWorkers = diagnostic.NewDiagnosticMetric(
	"VK0069",
	"vulkan.worker.state.failing_workers",
	string(MetricKindGauge),
	string(MetricUnitCount("worker")),
	"workers with a live instance on a nonzero consecutive failure streak",
	diagnostic.MetricScopeSystem,
)
