package controller

import (
	"errors"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/topic"
)

// TopicConfig is RegisterTopic's spec -- separate from Topic so Register can grow
// (retention, etc.) without a signature change.
type TopicConfig struct {
	// PartitionSize - rows per partition.
	// Default: 1_000_000.
	//
	// Lower values give finer-grained retention drops at the cost of more
	// partitions to maintain. Tune down for low-volume topics, up for
	// high-throughput ones.
	// Ex: 10_000 for a low-volume audit topic, 5_000_000 for high-throughput ingest.
	PartitionSize int64

	// RetentionTTL - how long a message survives before the janitor may drop
	// or sweep it.
	// Default: 0 (keep every message indefinitely).
	//
	// Set this once a topic has a real expiry requirement.
	// Ex: 30 * 24 * time.Hour for a 30-day event stream.
	RetentionTTL time.Duration

	// AllowDropPastCommitted - if true, retention can drop data a lagging
	// consumer group hasn't committed yet (Kafka's default behavior).
	// Default: false.
	//
	// Set true only if a badly-lagging consumer should lose data rather than
	// block cleanup.
	// Ex: true for a metrics topic where staleness beats unbounded disk growth.
	AllowDropPastCommitted bool

	// IdempotencyKeyTTL - how long a produce-retry claim survives in
	// idempotency_key before the janitor sweeps it.
	// Default: 1h.
	//
	// Zero is invalid, not "forever" -- WithDefaults resolves it before the
	// topic is ever registered. TTL only needs to cover your retry horizon,
	// not a retention window. Lower it for a topic whose producers never
	// retry across a restart.
	// Ex: 10 * time.Minute.
	IdempotencyKeyTTL time.Duration

	// DeliveryLogMode - which delivery outcomes write to delivery_log_<id>, the
	// per-attempt audit trail.
	// Default: DeliveryLogModeFailures (every outcome except success).
	//
	// DeliveryLogModeOff for a topic whose failure volume would make the extra
	// per-attempt write not worth paying for.
	// DeliveryLogModeAll when successes must be auditable per message
	// each success txn then also writes its 'success' row.
	DeliveryLogMode topic.DeliveryLogMode
}

func (c *TopicConfig) WithDefaults() *TopicConfig {
	if c.PartitionSize == 0 {
		c.PartitionSize = 1_000_000
	}
	if c.IdempotencyKeyTTL == 0 {
		c.IdempotencyKeyTTL = time.Hour
	}
	if c.DeliveryLogMode == "" {
		c.DeliveryLogMode = topic.DeliveryLogModeFailures
	}
	return c
}

// Validate runs after WithDefaults -- anything still out of range here was
// set by the caller, not left unset.
func (c *TopicConfig) Validate() error {
	// 1 makes every id a partition boundary; <= 0 breaks the DDL range
	if c.PartitionSize < 2 {
		return fmt.Errorf("PartitionSize must be >= 2, got %d", c.PartitionSize)
	}
	if c.RetentionTTL < 0 {
		return fmt.Errorf("RetentionTTL must be >= 0, got %v", c.RetentionTTL)
	}
	if c.IdempotencyKeyTTL < 0 {
		return fmt.Errorf("IdempotencyKeyTTL must be >= 0, got %v", c.IdempotencyKeyTTL)
	}
	if err := validateDeliveryLogMode(c.DeliveryLogMode); err != nil {
		return err
	}
	return nil
}

// AlterTopicConfig is Alter's sparse patch -- a nil field means leave unchanged.
// PartitionSize is absent -- currently immutable (future work)
// Name is absent -- renaming is its own verb, not a config change.
type AlterTopicConfig struct {
	RetentionTTL           *time.Duration
	AllowDropPastCommitted *bool
	IdempotencyKeyTTL      *time.Duration
	DeliveryLogMode        *topic.DeliveryLogMode
}

func (c *AlterTopicConfig) Validate() error {
	if c.RetentionTTL == nil && c.AllowDropPastCommitted == nil && c.IdempotencyKeyTTL == nil &&
		c.DeliveryLogMode == nil {
		return errors.New("no fields set -- an alter must change at least one field")
	}
	if c.RetentionTTL != nil && *c.RetentionTTL < 0 {
		return fmt.Errorf("RetentionTTL must be >= 0, got %v", *c.RetentionTTL)
	}
	if c.IdempotencyKeyTTL != nil && *c.IdempotencyKeyTTL <= 0 {
		return fmt.Errorf("IdempotencyKeyTTL must be > 0, got %v", *c.IdempotencyKeyTTL)
	}
	if c.DeliveryLogMode != nil {
		if err := validateDeliveryLogMode(*c.DeliveryLogMode); err != nil {
			return err
		}
	}
	return nil
}

func (c *TopicConfig) ToTopic(id int64, systemId int64, name string, version topic.SchemaVersion) *topic.Topic {
	return &topic.Topic{
		Id:                     id,
		SystemId:               systemId,
		Name:                   name,
		SchemaVersion:          version,
		PartitionSize:          c.PartitionSize,
		RetentionTTL:           c.RetentionTTL,
		AllowDropPastCommitted: c.AllowDropPastCommitted,
		IdempotencyKeyTTL:      c.IdempotencyKeyTTL,
		DeliveryLogMode:        c.DeliveryLogMode,
	}
}

func validateDeliveryLogMode(deliveryLogMode topic.DeliveryLogMode) error {
	switch deliveryLogMode {
	case topic.DeliveryLogModeOff, topic.DeliveryLogModeFailures, topic.DeliveryLogModeAll:
		return nil
	}
	return fmt.Errorf("DeliveryLogMode must be %q, %q, or %q, got %q", topic.DeliveryLogModeOff, topic.DeliveryLogModeFailures, topic.DeliveryLogModeAll, deliveryLogMode)
}
