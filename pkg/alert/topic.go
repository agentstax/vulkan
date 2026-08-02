package alert

import (
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/topic"
)

// TopicName is __system.alerts
const TopicName = common.SystemTopicPrefix + "alerts"

func TopicConfig() *topic.Config {
	return &topic.Config{
		PartitionSize:      10_000,
		RetentionTTL:       7 * 24 * time.Hour,
		DisableDeliveryLog: true,
	}
}
