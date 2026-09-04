package metrics

import (
	"github.com/agentstax/vulkan/pkg/common"
)

// TopicName is __system.metrics
const TopicName = common.SystemTopicPrefix + "metrics"

type TopicSnapshot struct {
	TopicId   int64                   `json:"topic_id"`
	Compacted bool                    `json:"compacted"`
	Groups    []ConsumerGroupSnapshot `json:"groups"`
}

// TopicSchemaVersionSnapshot is one payload version's presence in a topic's log.
type TopicSchemaVersionSnapshot struct {
	Version         int                             `json:"version"`
	Messages        int64                           `json:"messages"`
	CompactionHeads int64                           `json:"compaction_heads"` // keys whose current head is at this version
	Groups          []ConsumerGroupSchemaVersionLag `json:"groups"`
}

// ConsumerGroupSchemaVersionLag is one consumer group's unread and unresolved rows
// at one payload version.
type ConsumerGroupSchemaVersionLag struct {
	ConsumerGroup        string `json:"group"`
	Unconsumed           int64  `json:"unconsumed"` // rows at the version above the group's committed cursor
	UnresolvedExceptions int64  `json:"unresolved_exceptions"`
}
