package metrics

import "github.com/agentstax/vulkan/pkg/common"

// TopicName is __system.metrics
const TopicName = common.SystemTopicPrefix + "metrics"

type TopicSnapshot struct {
	TopicId   int64                   `json:"topic_id"`
	Compacted bool                    `json:"compacted"`
	Groups    []ConsumerGroupSnapshot `json:"groups"`
}
