package controller

import (
	"time"

	"github.com/agentstax/vulkan/pkg/topic"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
)

func TopicConfig() *topiccontroller.TopicConfig {
	return &topiccontroller.TopicConfig{
		PartitionSize: 10_000,

		// retention is the measurement history window: how far back a series
		// reads, and how long a series that stopped reporting keeps its head
		RetentionTTL:           24 * time.Hour,
		AllowDropPastCommitted: true, // a lagging metrics consumer loses measurements rather than blocking cleanup
		DeliveryLogMode:        topic.DeliveryLogModeOff,
	}
}
