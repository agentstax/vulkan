package common

import "time"

// MessageData is one stored message.
type MessageData[Message any] struct {
	Id             int64     `json:"message_id"`
	Message        *Message  `json:"message"`
	CreatedAt      time.Time `json:"created_at"`
	RoutingKey     string    `json:"routing_key"`
	MessageKey     string    `json:"message_key"`
	CompactionRank int64     `json:"compaction_rank"`
}
