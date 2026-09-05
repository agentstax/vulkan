package metrics

import "github.com/agentstax/vulkan/pkg/common/diagnostic"

var MetricCursorHead = diagnostic.NewDiagnosticMetric(
	"VK0080",
	"vulkan.consumer.cursor.head",
	string(MetricKindGauge),
	string(MetricUnitCount("message")),
	"highest message id ever appended to the topic",
	diagnostic.MetricScopeConsumerGroup,
	"topic",
	"group",
)

var MetricCursorClaimed = diagnostic.NewDiagnosticMetric(
	"VK0081",
	"vulkan.consumer.cursor.claimed",
	string(MetricKindGauge),
	string(MetricUnitCount("message")),
	"the consumer group's read frontier",
	diagnostic.MetricScopeConsumerGroup,
	"topic",
	"group",
)

var MetricCursorCommitted = diagnostic.NewDiagnosticMetric(
	"VK0082",
	"vulkan.consumer.cursor.committed",
	string(MetricKindGauge),
	string(MetricUnitCount("message")),
	"the frontier at or below which every message is complete or dead",
	diagnostic.MetricScopeConsumerGroup,
	"topic",
	"group",
)

var MetricCursorBacklog = diagnostic.NewDiagnosticMetric(
	"VK0083",
	"vulkan.consumer.cursor.backlog",
	string(MetricKindGauge),
	string(MetricUnitCount("message")),
	"messages beyond the consumer group's committed frontier",
	diagnostic.MetricScopeConsumerGroup,
	"topic",
	"group",
)

var MetricCursorInflight = diagnostic.NewDiagnosticMetric(
	"VK0084",
	"vulkan.consumer.cursor.inflight",
	string(MetricKindGauge),
	string(MetricUnitCount("message")),
	"claimed messages beyond the consumer group's committed frontier",
	diagnostic.MetricScopeConsumerGroup,
	"topic",
	"group",
)

var MetricReadyExceptions = diagnostic.NewDiagnosticMetric(
	"VK0085",
	"vulkan.consumer.exceptions.ready",
	string(MetricKindGauge),
	string(MetricUnitCount("exception")),
	"delivery rows waiting for their retry delay",
	diagnostic.MetricScopeConsumerGroup,
	"topic",
	"group",
)

var MetricInflightExceptions = diagnostic.NewDiagnosticMetric(
	"VK0086",
	"vulkan.consumer.exceptions.inflight",
	string(MetricKindGauge),
	string(MetricUnitCount("exception")),
	"delivery rows held by a retry attempt",
	diagnostic.MetricScopeConsumerGroup,
	"topic",
	"group",
)

var MetricDeferredExceptions = diagnostic.NewDiagnosticMetric(
	"VK0087",
	"vulkan.consumer.exceptions.deferred",
	string(MetricKindGauge),
	string(MetricUnitCount("exception")),
	"delivery rows waiting for their message key's lease to become free",
	diagnostic.MetricScopeConsumerGroup,
	"topic",
	"group",
)

var MetricDeadExceptions = diagnostic.NewDiagnosticMetric(
	"VK0088",
	"vulkan.consumer.exceptions.dead",
	string(MetricKindGauge),
	string(MetricUnitCount("exception")),
	"dead-lettered delivery rows",
	diagnostic.MetricScopeConsumerGroup,
	"topic",
	"group",
)

var MetricOldestUnresolvedAge = diagnostic.NewDiagnosticMetric(
	"VK0089",
	"vulkan.consumer.exceptions.oldest_unresolved_age",
	string(MetricKindGauge),
	string(MetricUnitMilliseconds),
	"age of the oldest ready, inflight, or deferred delivery row",
	diagnostic.MetricScopeConsumerGroup,
	"topic",
	"group",
)

var MetricOpenLeases = diagnostic.NewDiagnosticMetric(
	"VK0090",
	"vulkan.consumer.open_leases",
	string(MetricKindGauge),
	string(MetricUnitCount("lease")),
	"open leases held by the consumer group",
	diagnostic.MetricScopeConsumerGroup,
	"topic",
	"group",
)

var MetricAbandonedOutstanding = diagnostic.NewDiagnosticMetric(
	"VK0091",
	"vulkan.consumer.abandoned_routines.outstanding",
	string(MetricKindGauge),
	string(MetricUnitCount("routine")),
	"abandoned routines with no matching cleared record",
	diagnostic.MetricScopeConsumerGroup,
	"topic",
	"group",
)

var MetricAbandonedTotal = diagnostic.NewDiagnosticMetric(
	"VK0092",
	"vulkan.consumer.abandoned_routines.total",
	string(MetricKindGauge),
	string(MetricUnitCount("routine")),
	"distinct abandoned routines retained on the metrics topic",
	diagnostic.MetricScopeConsumerGroup,
	"topic",
	"group",
)

var MetricAbandonedSelfClearLatencyAvg = diagnostic.NewDiagnosticMetric(
	"VK0093",
	"vulkan.consumer.abandoned_routines.self_clear_latency_avg",
	string(MetricKindGauge),
	string(MetricUnitMilliseconds),
	"mean time between an abandoned routine and its matching cleared record",
	diagnostic.MetricScopeConsumerGroup,
	"topic",
	"group",
)
