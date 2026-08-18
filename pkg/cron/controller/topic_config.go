package controller

import (
	"time"

	"github.com/agentstax/vulkan/pkg/topic"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
)

// TopicConfig - RetentionTTL is the job-request-history horizon; it must
// exceed the widest schedule rate (monthly covered) so a job's next request
// lands before its last one ages out.
// DeliveryLogModeAll keeps a 'success' row per request - job status uses this.
func TopicConfig() *topiccontroller.TopicConfig {
	return &topiccontroller.TopicConfig{
		PartitionSize:   10_000,
		RetentionTTL:    35 * 24 * time.Hour,
		DeliveryLogMode: topic.DeliveryLogModeAll,
	}
}
