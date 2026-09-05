package metrics

import "github.com/agentstax/vulkan/pkg/common/diagnostic"

// Consumer-session flows are per-instance monotonic totals, one series per
// session. They report what one instance did.
var MetricSessionClaimed = diagnostic.NewDiagnosticMetric(
	"VK0042",
	"vulkan.consumer.session.claimed",
	string(MetricKindCounter),
	string(MetricUnitCount("message")),
	"messages this instance claimed -- cursor ranges and exception retries together",
	diagnostic.MetricScopeConsumerSession,
	"topic",
	"group",
	"version",
	"session",
)

var MetricSessionSuccess = diagnostic.NewDiagnosticMetric(
	"VK0043",
	"vulkan.consumer.session.success",
	string(MetricKindCounter),
	string(MetricUnitCount("message")),
	"consumerFunc runs that completed cleanly, counted at resolution",
	diagnostic.MetricScopeConsumerSession,
	"topic",
	"group",
	"version",
	"session",
)

var MetricSessionSuperseded = diagnostic.NewDiagnosticMetric(
	"VK0044",
	"vulkan.consumer.session.superseded",
	string(MetricKindCounter),
	string(MetricUnitCount("message")),
	"messages resolved without running -- a newer version of their compacted message key had already arrived",
	diagnostic.MetricScopeConsumerSession,
	"topic",
	"group",
	"version",
	"session",
)

var MetricSessionReady = diagnostic.NewDiagnosticMetric(
	"VK0045",
	"vulkan.consumer.session.ready",
	string(MetricKindCounter),
	string(MetricUnitCount("message")),
	"delivery rows written 'ready' -- each will be retried once its backoff passes",
	diagnostic.MetricScopeConsumerSession,
	"topic",
	"group",
	"version",
	"session",
)

var MetricSessionDeferred = diagnostic.NewDiagnosticMetric(
	"VK0046",
	"vulkan.consumer.session.deferred",
	string(MetricKindCounter),
	string(MetricUnitCount("message")),
	"delivery rows written 'deferred' -- another delivery held their message key",
	diagnostic.MetricScopeConsumerSession,
	"topic",
	"group",
	"version",
	"session",
)

var MetricSessionDead = diagnostic.NewDiagnosticMetric(
	"VK0047",
	"vulkan.consumer.session.dead",
	string(MetricKindCounter),
	string(MetricUnitCount("message")),
	"delivery rows written 'dead' -- exhausted retries, unrecoverable payloads, and the kill backstop",
	diagnostic.MetricScopeConsumerSession,
	"topic",
	"group",
	"version",
	"session",
)

var MetricSessionReclaimed = diagnostic.NewDiagnosticMetric(
	"VK0048",
	"vulkan.consumer.session.reclaimed",
	string(MetricKindCounter),
	string(MetricUnitCount("lease")),
	"leases taken over from expired workers -- another instance died or stalled mid-range",
	diagnostic.MetricScopeConsumerSession,
	"topic",
	"group",
	"version",
	"session",
)

var MetricSessionQuarantined = diagnostic.NewDiagnosticMetric(
	"VK0049",
	"vulkan.consumer.session.quarantined",
	string(MetricKindCounter),
	string(MetricUnitCount("range")),
	"ranges past the reclaim cap, written out as independent 'ready' exceptions",
	diagnostic.MetricScopeConsumerSession,
	"topic",
	"group",
	"version",
	"session",
)

var MetricSessionAbandoned = diagnostic.NewDiagnosticMetric(
	"VK0050",
	"vulkan.consumer.session.abandoned",
	string(MetricKindCounter),
	string(MetricUnitCount("routine")),
	"consumerFunc goroutines written off past the hard timeout",
	diagnostic.MetricScopeConsumerSession,
	"topic",
	"group",
	"version",
	"session",
)

var MetricSessionLeaseLost = diagnostic.NewDiagnosticMetric(
	"VK0051",
	"vulkan.consumer.session.lease_lost",
	string(MetricKindCounter),
	string(MetricUnitCount("commit")),
	"commits rejected because another worker reclaimed the lease first -- that work was redone elsewhere",
	diagnostic.MetricScopeConsumerSession,
	"topic",
	"group",
	"version",
	"session",
)
