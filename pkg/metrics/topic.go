package metrics

import (
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/topic"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
)

// TopicName is __system.metrics
const TopicName = common.SystemTopicPrefix + "metrics"

func TopicConfig() *topiccontroller.TopicConfig {
	return &topiccontroller.TopicConfig{
		PartitionSize:   10_000,
		RetentionTTL:    24 * time.Hour,
		DeliveryLogMode: topic.DeliveryLogModeOff,
	}
}

type TopicSnapshot struct {
	TopicId   int64
	Compacted bool
	Groups    []ConsumerGroupSnapshot
}
