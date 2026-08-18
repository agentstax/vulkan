package metrics

import "github.com/agentstax/vulkan/pkg/common"

// TopicName is __system.metrics
const TopicName = common.SystemTopicPrefix + "metrics"

type TopicSnapshot struct {
	TopicId   int64
	Compacted bool
	Groups    []ConsumerGroupSnapshot
}
