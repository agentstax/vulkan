package controller

import (
	"time"

	"github.com/agentstax/vulkan/pkg/topic"
)

// TopicConfig - RetentionTTL is the schedule-request-history horizon; it must
// exceed the widest schedule rate (monthly covered) so a schedule's next request
// lands before its last one ages out.
// DeliveryLogModeAll keeps a 'success' row per request - schedule status uses this.
func TopicConfig() *topic.TopicConfig {
	return &topic.TopicConfig{
		PartitionSize:   10_000,
		RetentionTTL:    35 * 24 * time.Hour,
		DeliveryLogMode: topic.DeliveryLogModeAll,
	}
}
