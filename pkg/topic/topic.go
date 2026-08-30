package topic

import (
	"regexp"
	"time"
)

// topic name can't contain '*' as it's the binding wildcard
var SlugPattern = regexp.MustCompile(`^[a-z0-9._-]+$`)

// SchemaVersion is the payload's compatibility version, written on every
// message row from the Message type's own SchemaVersion method.
//
// Bump only on a BREAKING change to Message:
// - a field has a different type
// - a field has been renamed
// - a field has been removed
//
// A consumer group reads only rows at its Message type's version.
type SchemaVersion int64

// Versioned is what every Message type declares: the version its payload
// is. A value receiver, so the zero value answers.
type Versioned interface {
	SchemaVersion() SchemaVersion
}

// SchemaVersionOf reads the version a Message type declares.
func SchemaVersionOf[Message Versioned]() SchemaVersion {
	var message Message
	return message.SchemaVersion()
}

// DeliveryLogMode selects which delivery outcomes write delivery_log_<id> rows.
type DeliveryLogMode string

const (
	DeliveryLogModeOff      DeliveryLogMode = "off"      // no rows at all
	DeliveryLogModeFailures DeliveryLogMode = "failures" // every outcome except success
	DeliveryLogModeAll      DeliveryLogMode = "all"      // every outcome, including a 'success' row per success
)

// Id addresses this topic's own message_log_<id>.
type Topic struct {
	Id                     int64           `json:"topic_id"`
	SystemId               int64           `json:"system_id"`
	Name                   string          `json:"topic"`
	PartitionSize          int64           `json:"partition_size"`
	RetentionTTL           time.Duration   `json:"retention_ttl"`
	AllowDropPastCommitted bool            `json:"allow_drop_past_committed"`
	IdempotencyKeyTTL      time.Duration   `json:"idempotency_key_ttl"`
	DeliveryLogMode        DeliveryLogMode `json:"delivery_log_mode"`
}
