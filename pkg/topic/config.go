package topic

import (
	"errors"
	"os"
	"time"

	"github.com/agentstax/vulkan/pkg/logger"
)

// Config is separate from Topic so Register can grow (retention, etc.) without a signature change.
type Config struct {
	// Name - stable, unique identifier for this topic.
	// Default: none (required).
	//
	// Dot-namespaced by domain and entity: <domain>.<entity>[.<event>]. Safe
	// to rename later -- topics are addressed by id internally, not name.
	// Ex: "orders.created", "billing.invoice.paid"
	Name string

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
	// Default: 24h.
	//
	// Zero is invalid, not "forever" -- SetDefaults resolves it before the
	// topic is ever registered. Lower it for a topic whose producers never
	// retry across a restart.
	// Ex: 10 * time.Minute.
	IdempotencyKeyTTL time.Duration

	// DisableDeliveryLog - opt out of delivery_log_<id>, the per-attempt
	// failure audit trail written alongside every delivery_<id> failure.
	// Default: false (enabled).
	//
	// Set true for a topic whose failure volume would make the extra
	// per-attempt write not worth paying for.
	DisableDeliveryLog bool

	// PartitionSafetyBuffer - how far past the current write head the
	// janitor creates partitions ahead of time.
	// Default: 50_000.
	PartitionSafetyBuffer int64

	// JanitorPollRate - how often the janitor loop ticks (create-ahead,
	// drop/sweep expired partitions, sweep idempotency_key).
	// Default: 5 * time.Second.
	JanitorPollRate time.Duration

	// JanitorSweepBatchSize - rows deleted per sweep transaction; caps how
	// much of a backlog one batch holds a lock for.
	// Default: 1000.
	JanitorSweepBatchSize int

	// Logger - pass your own *slog.Logger (own Handler) or anything satisfying
	// logger.Logger.
	// Default: a text logger to os.Stdout at warn level and up.
	//
	// Only takes effect for Register -- Destroy and Exists have no Config
	// parameter to carry one, so they always use that same default.
	Logger logger.Logger
}

func (c *Config) SetDefaults() {
	if c.PartitionSize == 0 {
		c.PartitionSize = 1_000_000
	}
	if c.IdempotencyKeyTTL == 0 {
		c.IdempotencyKeyTTL = 24 * time.Hour
	}
	if c.PartitionSafetyBuffer == 0 {
		c.PartitionSafetyBuffer = 50000 // TODO - determine sane default
	}
	if c.JanitorPollRate == 0 {
		c.JanitorPollRate = 5 * time.Second
	}
	if c.JanitorSweepBatchSize == 0 {
		c.JanitorSweepBatchSize = 1000
	}
	if c.Logger == nil {
		c.Logger = logger.NewDefaultLogger(os.Stdout)
	}
}

func (c *Config) Validate() error {
	if err := validateName(c.Name); err != nil {
		return err
	}
	if c.RetentionTTL < 0 {
		return errors.New("RetentionTTL must be >= 0")
	}
	if c.IdempotencyKeyTTL < 0 {
		return errors.New("IdempotencyKeyTTL must be >= 0")
	}
	if c.PartitionSafetyBuffer < 0 {
		return errors.New("PartitionSafetyBuffer must be >= 0")
	}
	if c.JanitorPollRate < 0 {
		return errors.New("JanitorPollRate must be >= 0")
	}
	if c.JanitorSweepBatchSize < 0 {
		return errors.New("JanitorSweepBatchSize must be >= 0")
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
		IdempotencyKeyTTL:      c.IdempotencyKeyTTL,
		DisableDeliveryLog:     c.DisableDeliveryLog,
		PartitionSafetyBuffer:  c.PartitionSafetyBuffer,
		JanitorPollRate:        c.JanitorPollRate,
		JanitorSweepBatchSize:  c.JanitorSweepBatchSize,
	}
}
