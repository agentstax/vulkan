package topic

import (
	"time"
)

// SchemaVersion is a topic's version under a shared name.
// A version bump is a whole new physical topic.
type SchemaVersion int64

// Id addresses this topic's own message_log_<id>.
type Topic struct {
	Id                     int64
	Name                   string
	SchemaVersion          SchemaVersion
	PartitionSize          int64
	RetentionTTL           time.Duration
	AllowDropPastCommitted bool
	IdempotencyKeyTTL      time.Duration
	DisableDeliveryLog     bool
	JanitorPollRate        time.Duration
	JanitorSweepBatchSize  int
	WaterlinePollRate      time.Duration
	CreatedAt              time.Time
	UpdatedAt              time.Time
}
