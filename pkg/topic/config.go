package topic

import (
	"errors"
	"time"
)

// Config is separate from Topic so Register can grow (retention, etc.) without a signature change.
type Config struct {
	// Name - stable, unique identifier for this topic.
	//
	// Standard pattern is dot-namespaced by domain and entity: <domain>.<entity>[.<event>].
	// Renaming later is safe -- topics are addressed by id internally, not name.
	// Ex: "orders.created", "billing.invoice.paid"
	Name string

	// PartitionSize - rows per partition. Defaults to 1_000_000 if left unset.
	//
	// Lower values give finer-grained retention drops at the cost of more
	// partitions to maintain; higher values are coarser but cheaper to manage.
	// Tune down for low-volume topics, up for high-throughput ones.
	// Ex: 10_000 for a low-volume audit topic, 5_000_000 for high-throughput ingest.
	PartitionSize int64

	// RetentionTTL - how long a message survives before the janitor may drop
	// or sweep it. Zero disables retention entirely (the default).
	//
	// Set this once data has an actual expiry requirement; leave unset for
	// topics that should keep every message indefinitely.
	// Ex: 0 for an audit log, 30 * 24 * time.Hour for a 30-day event stream.
	RetentionTTL time.Duration

	// AllowDropPastCommitted - opts into Kafka's "lagging consumer falls off
	// the retention window" semantics. False (the default) is the safe
	// floor: retention never drops data a consumer group hasn't committed.
	//
	// Only set true if a badly-lagging consumer should lose data rather than
	// block cleanup.
	// Ex: false for most topics, true for a metrics topic where staleness
	// beats unbounded disk growth.
	AllowDropPastCommitted bool
}

func (c *Config) SetDefaults() {
	if c.PartitionSize == 0 {
		c.PartitionSize = 1_000_000
	}
}

func (c *Config) Validate() error {
	if err := validateName(c.Name); err != nil {
		return err
	}
	if c.RetentionTTL < 0 {
		return errors.New("RetentionTTL must be >= 0")
	}
	return nil
}

func (c *Config) ToTopic(id int64) *Topic {
	return &Topic{
		Id:                     id,
		Name:                   c.Name,
		PartitionSize:          c.PartitionSize,
		RetentionTTL:           c.RetentionTTL,
		AllowDropPastCommitted: c.AllowDropPastCommitted,
	}
}
