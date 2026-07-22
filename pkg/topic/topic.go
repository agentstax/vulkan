package topic

import (
	"time"
)

// Id addresses this topic's own message_log_<id>.
type Topic struct {
	Id                     int64
	Name                   string
	PartitionSize          int64
	RetentionTTL           time.Duration
	AllowDropPastCommitted bool
	IdempotencyKeyTTL      time.Duration
	DisableDeliveryLog     bool
	JanitorPollRate        time.Duration
	JanitorSweepBatchSize  int
}
