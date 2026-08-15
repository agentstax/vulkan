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
	DeliveryLogMode        string
	CreatedAt              time.Time
	UpdatedAt              time.Time
}
