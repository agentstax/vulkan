package datastore

import (
	"encoding/json"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/jackc/pgx/v5/pgtype"
)

// KeyLeaseData is the key claim an outcome releases in its own transaction --
// the token match means a lease that expired and was taken over is never
// released by the old holder.
type KeyLeaseData struct {
	ConsumerGroupId int64
	CompactionKey   string
	Token           pgtype.UUID
}

// one exception claimed off the exception window for (re)processing -- the lease
// token guards its resolution: every write against the row matches on it.
type ExceptionData struct {
	ConsumerGroupId int64                  `db:"consumer_group_id"`
	TopicID         int64                  `db:"topic_id"`
	MessageId       int64                  `db:"message_id"`
	Attempts        int                    `db:"attempts"`
	LeaseToken      pgtype.UUID            `db:"lease_token"`
	LeaseUntil      time.Time              `db:"lease_until"`
	Payload         json.RawMessage        `db:"payload"`
	CreatedAt       time.Time              `db:"created_at"`
	RoutingKey      string                 `db:"routing_key"`
	CompactionKey   string                 `db:"compaction_key"`
	CompactionRank  int64                  `db:"compaction_rank"`
	Options         *common.MessageOptions `db:"options"`
}
