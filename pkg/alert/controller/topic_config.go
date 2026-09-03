package controller

import (
	"time"

	"github.com/agentstax/vulkan/pkg/topic"
)

func TopicConfig() *topic.TopicConfig {
	return &topic.TopicConfig{
		PartitionSize:   10_000,
		RetentionTTL:    7 * 24 * time.Hour,
		DeliveryLogMode: topic.DeliveryLogModeOff,
	}
}
