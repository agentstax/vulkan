package topic

import (
	"regexp"
	"time"
)

// topic name can't contain '*' as it's the binding wildcard
var slugPattern = regexp.MustCompile(`^[a-z0-9._-]+$`)

// SchemaVersion is a contract for a topic's message compatibility.
//
// Bump only on a BREAKING change to Message:
// - a field has a different type
// - a field has been renamed
// - a field has been removed
//
// Bumping SchemaVersion creates a brand new physical
// topic under the same name.
type SchemaVersion int64

// Id addresses this topic's own message_log_<id>.
type Topic struct {
	Id                     int64
	SystemId               int64
	Name                   string
	SchemaVersion          SchemaVersion
	PartitionSize          int64
	RetentionTTL           time.Duration
	AllowDropPastCommitted bool
	IdempotencyKeyTTL      time.Duration
	DisableDeliveryLog     bool
	CreatedAt              time.Time
	UpdatedAt              time.Time
}
