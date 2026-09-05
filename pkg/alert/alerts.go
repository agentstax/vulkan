package alert

import (
	"github.com/agentstax/vulkan/pkg/common/diagnostic"
)

// The built-in alerts. Every check builds a topic owner today, so all three
// are topic-scoped; the name is the value on the wire and the message key's
// first segment.
var AlertPartitionCount = diagnostic.NewDiagnosticAlert("VK0094",
	"partition_count",
	"a topic's message log holds enough partitions that dropping the topic approaches the lock-table ceiling",
	diagnostic.MetricScopeTopic, string(AlertSeverityWarn))

var AlertCompactionReadCost = diagnostic.NewDiagnosticAlert("VK0095",
	"compaction_read_cost",
	"a compacted topic holds enough partitions that replaying a never-superseded key is a long scan",
	diagnostic.MetricScopeTopic, string(AlertSeverityWarn))

var AlertWorkerLiveness = diagnostic.NewDiagnosticAlert("VK0096",
	"worker_liveness",
	"a topic's worker rows have no live instance, so nothing runs its upkeep",
	diagnostic.MetricScopeTopic, string(AlertSeverityWarn))
