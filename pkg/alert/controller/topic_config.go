package controller

import (
	"time"

	"github.com/agentstax/vulkan/pkg/topic"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
)

func TopicConfig() *topiccontroller.TopicConfig {
	return &topiccontroller.TopicConfig{
		PartitionSize:   10_000,
		RetentionTTL:    7 * 24 * time.Hour,
		DeliveryLogMode: topic.DeliveryLogModeOff,
	}
}
