package topic

import (
	"fmt"
	"os"
	"time"

	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/retry"
)

// Config is separate from Topic so Register can grow (retention, etc.) without a signature change.
type Config struct {
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

	// DisableDeliveryLog - don't record to delivery_log_<id>, the per-attempt
	// failure audit trail written alongside every delivery_<id> failure.
	// Default: false (enabled).
	//
	// Set true for a topic whose failure volume would make the extra
	// per-attempt write not worth paying for.
	DisableDeliveryLog bool

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

	// Retry - transient-error retry policy for this topic's own datastore calls.
	// Default: retry.NewDefaultRetryPolicy().
	Retry *retry.Policy
}

func (c *Config) WithDefaults() *Config {
	if c.PartitionSize == 0 {
		c.PartitionSize = 1_000_000
	}
	if c.IdempotencyKeyTTL == 0 {
		c.IdempotencyKeyTTL = time.Hour
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
	c.Retry = c.Retry.WithDefaults()
	return c
}

// Validate runs after WithDefaults -- anything still out of range here was
// set by the caller, not left unset.
func (c *Config) Validate() error {
	if c.RetentionTTL < 0 {
		return fmt.Errorf("RetentionTTL must be >= 0, got %v", c.RetentionTTL)
	}
	if c.IdempotencyKeyTTL < 0 {
		return fmt.Errorf("IdempotencyKeyTTL must be >= 0, got %v", c.IdempotencyKeyTTL)
	}
	if c.JanitorPollRate < 0 {
		return fmt.Errorf("JanitorPollRate must be >= 0, got %v", c.JanitorPollRate)
	}
	if c.JanitorSweepBatchSize < 0 {
		return fmt.Errorf("JanitorSweepBatchSize must be >= 0, got %d", c.JanitorSweepBatchSize)
	}
	if err := c.Retry.Validate(); err != nil {
		return fmt.Errorf("Retry: %w", err)
	}
	return nil
}

func (c *Config) ToTopic(id int64, name string, createdAt, updatedAt time.Time) *Topic {
	return &Topic{
		Id:                     id,
		Name:                   name,
		PartitionSize:          c.PartitionSize,
		RetentionTTL:           c.RetentionTTL,
		AllowDropPastCommitted: c.AllowDropPastCommitted,
		IdempotencyKeyTTL:      c.IdempotencyKeyTTL,
		DisableDeliveryLog:     c.DisableDeliveryLog,
		JanitorPollRate:        c.JanitorPollRate,
		JanitorSweepBatchSize:  c.JanitorSweepBatchSize,
		CreatedAt:              createdAt,
		UpdatedAt:              updatedAt,
	}
}
