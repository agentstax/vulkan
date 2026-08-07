package datastore

import (
	"time"
)

// TopicData models the topic table row exactly.
type TopicData struct {
	Id                     int64
	SystemId               int64
	Name                   string
	SchemaVersion          int64
	PartitionSize          int64
	RetentionTTLNs         int64
	AllowDropPastCommitted bool
	IdempotencyKeyTTLNs    int64
	DisableDeliveryLog     bool
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

// AlterTopicData is UpdateTopic's sparse patch -- a nil field means leave
// the column unchanged.
type AlterTopicData struct {
	RetentionTTLNs         *int64
	AllowDropPastCommitted *bool
	IdempotencyKeyTTLNs    *int64
	DisableDeliveryLog     *bool
}
