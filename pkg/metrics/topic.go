package metrics

import (
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/topic"
)

// TopicName is __system.metrics
const TopicName = common.SystemTopicPrefix + "metrics"

func TopicConfig() *topic.Config {
	return &topic.Config{
		PartitionSize:      10_000,
		RetentionTTL:       24 * time.Hour,
		DisableDeliveryLog: true,
	}
}
