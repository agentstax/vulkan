package datastore

import (
	"encoding/json"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/google/uuid"
)

// AppendData is one message append's inputs, insert-arg exact.
type AppendData[Message any] struct {
	// resolved once by the caller and reused across every retry -- that reuse
	// is what makes a retried attempt safe after an ambiguous commit
	IdempotencyKey uuid.UUID
	Payload        *Message // nil when a ProduceFunc supplies it inside the transaction
	RoutingKey     string
	CompactionKey  string
	CompactionRank int64
	Options        *common.MessageOptions
}

// AppendedData is one append's outcome.
type AppendedData[Message any] struct {
	Message   *Message // the payload this call built -- never re-read from storage
	Id        int64    // 0 when Duplicate
	Duplicate bool     // the idempotency claim already existed
}

// HeadData is the compaction head's message row, column-exact.
type HeadData struct {
	Id             int64           `db:"id"`
	Payload        json.RawMessage `db:"payload"`
	CreatedAt      time.Time       `db:"created_at"`
	RoutingKey     string          `db:"routing_key"` // "" if unset, COALESCE'd at read
	CompactionKey  string          `db:"compaction_key"`
	CompactionRank int64           `db:"compaction_rank"`
}
