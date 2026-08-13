package datastore

import (
	"encoding/json"
	"time"
)

// HeadData is the compaction head's message row, column-exact.
type HeadData struct {
	Id             int64           `db:"id"`
	Payload        json.RawMessage `db:"payload"`
	CreatedAt      time.Time       `db:"created_at"`
	RoutingKey     string          `db:"routing_key"` // "" if unset, COALESCE'd at read
	CompactionKey  string          `db:"compaction_key"`
	CompactionRank int64           `db:"compaction_rank"`
}
