package alert

import (
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
)

// TopicName is __system.alerts
const TopicName = common.SystemTopicPrefix + "alerts"

func TopicConfig() *topiccontroller.TopicConfig {
	return &topiccontroller.TopicConfig{
		PartitionSize:      10_000,
		RetentionTTL:       7 * 24 * time.Hour,
		DisableDeliveryLog: true,
	}
}
