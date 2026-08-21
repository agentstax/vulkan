package datastore

import (
	"encoding/json"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/jackc/pgx/v5/pgtype"
)

type MessageData struct {
	Id             int64                  `db:"id"`
	Payload        json.RawMessage        `db:"payload"`
	CreatedAt      time.Time              `db:"created_at"`
	RoutingKey     string                 `db:"routing_key"`    // "" if unset, COALESCE'd at read
	CompactionKey  string                 `db:"compaction_key"` // "" if unset, COALESCE'd at read
	CompactionRank int64                  `db:"compaction_rank"`
	Options        *common.MessageOptions `db:"options"`
}

type LeaseData struct {
	Token           pgtype.UUID `db:"token"`
	ConsumerGroupId int64       `db:"consumer_group_id"`
	Low             int64       `db:"low"`
	High            int64       `db:"high"`
	Until           time.Time   `db:"until"`
	Reclaims        int         `db:"reclaims"`
}

// a leased window of work -- the messages to process plus the lease that guards
// them. the worker frees the lease (Commit) once the whole range is done; the
// lazy roller then advances committed past it.
type ClaimedRangeData struct {
	Lease    LeaseData
	Messages []MessageData

	// Quarantined -> the reclaimable range hit max reclaims and was written
	// out as 'ready' exceptions instead; Messages is empty and the lease is
	// already freed. Nothing to dispatch or commit.
	Quarantined bool
}

// OutcomeKind is how one message of a claimed range resolved.
type OutcomeKind string

const (
	OutcomeException  OutcomeKind = "exception"  // retryable -- writes a 'ready' delivery row instead of failing the whole range
	OutcomeTerminal   OutcomeKind = "terminal"   // no retry could ever succeed -- writes the delivery row straight to 'dead'
	OutcomeSuperseded OutcomeKind = "superseded" // a newer message on its compaction key exists -- log row only, never a delivery row
	OutcomeDeferred   OutcomeKind = "deferred"   // another delivery held its key -- writes a 'deferred' delivery row for the exception window
	OutcomeSuccess    OutcomeKind = "success"    // ran clean -- log row only, never a delivery row; callers include it only under DeliveryLogModeAll
)

// OutcomeData is one resolved message of a claimed range.
type OutcomeData struct {
	MessageId int64
	Kind      OutcomeKind
	Err       string
}

// Low == High means cursor exists but is already at the proven head (nothing to claim)
type CursorData struct {
	Low  int64 `db:"low"`
	High int64 `db:"high"`
}
