package datastore

import (
	"time"
)

// TopicConfigRow models the topic_config table row exactly.
type TopicConfigRow struct {
	Id                     int64     `db:"id"`
	SystemId               int64     `db:"system_id"`
	Name                   string    `db:"name"`
	PartitionSize          int64     `db:"partition_size"`
	RetentionTTLNs         int64     `db:"retention_ttl_ns"`
	AllowDropPastCommitted bool      `db:"allow_drop_past_committed"`
	IdempotencyKeyTTLNs    int64     `db:"idempotency_key_ttl_ns"`
	DeliveryLogMode        string    `db:"delivery_log_mode"`
	CreatedAt              time.Time `db:"created_at"`
	UpdatedAt              time.Time `db:"updated_at"`
}
