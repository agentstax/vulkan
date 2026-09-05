package common

import "time"

// StoredMessage is one message as the log holds it: the payload plus the
// row's own facts.
type StoredMessage[Message any] struct {
	Id             int64     `json:"message_id"`
	Message        *Message  `json:"message"`
	CreatedAt      time.Time `json:"created_at"`
	RoutingKey     string    `json:"routing_key"`
	MessageKey     string    `json:"message_key"`
	CompactionRank int64     `json:"compaction_rank"`
}
