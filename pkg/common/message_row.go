package common

import "time"

// MessageRow is one stored message.
type MessageRow[Message any] struct {
	Id             int64     `json:"message_id"`
	Message        *Message  `json:"message"`
	CreatedAt      time.Time `json:"created_at"`
	RoutingKey     string    `json:"routing_key"`
	CompactionKey  string    `json:"compaction_key"`
	CompactionRank int64     `json:"compaction_rank"`
}
