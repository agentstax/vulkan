package datastore

import (
	"encoding/json"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/jackc/pgx/v5/pgtype"
)

// KeyLease is the key claim an outcome releases in its own transaction --
// the token match means a lease that expired and was taken over is never
// released by the old holder.
type KeyLease struct {
	TopicId         int64
	ConsumerGroupId int64
	MessageKey      string
	Token           pgtype.UUID
}

// one exception claimed off the exception window for (re)processing -- the lease
// token guards its resolution: every write against the row matches on it.
type ExceptionQueueRow struct {
	ConsumerGroupId int64                  `db:"consumer_group_id"`
	TopicId         int64                  `db:"topic_id"`
	MessageId       int64                  `db:"message_id"`
	Attempts        int                    `db:"attempts"`
	Delays          int                    `db:"delays"`
	LeaseToken      pgtype.UUID            `db:"lease_token"`
	LeaseExpiresAt  time.Time              `db:"lease_expires_at"`
	Payload         json.RawMessage        `db:"payload"`
	CreatedAt       time.Time              `db:"created_at"`
	RoutingKey      string                 `db:"routing_key"`
	MessageKey      string                 `db:"message_key"`
	CompactionRank  int64                  `db:"compaction_rank"` // 0 if not compacted, COALESCE'd at read
	Compacted       bool                   `db:"compacted"`       // compaction_rank IS NOT NULL, aliased at read
	Options         *common.MessageOptions `db:"options"`
}
