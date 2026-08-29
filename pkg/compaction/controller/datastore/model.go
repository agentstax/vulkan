package datastore

import (
	"encoding/json"
	"time"
)

// MessageData is one compacted message row.
type MessageData struct {
	Id             int64           `db:"id"`
	Payload        json.RawMessage `db:"payload"`
	CreatedAt      time.Time       `db:"created_at"`
	RoutingKey     string          `db:"routing_key"` // "" if unset, COALESCE'd at read
	MessageKey     string          `db:"message_key"`
	CompactionRank int64           `db:"compaction_rank"`
}
