package metrics

import "github.com/agentstax/vulkan/pkg/common/diagnostic"

var MetricTopicCompacted = diagnostic.NewDiagnosticMetric(
	"VK0079",
	"vulkan.topic.state.compacted",
	string(MetricKindGauge),
	"",
	"1 once the topic has received a keyed message, otherwise 0",
	diagnostic.MetricScopeTopic,
	"topic",
)
