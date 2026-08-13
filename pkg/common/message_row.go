package common

import "time"

// MessageRow is one stored message.
type MessageRow[Message any] struct {
	Id             int64
	Message        *Message
	CreatedAt      time.Time
	RoutingKey     string
	CompactionKey  string
	CompactionRank int64
}
