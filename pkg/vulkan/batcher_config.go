package vulkan

import (
	"time"
)

// BatcherConfig is ProducerConfig.Batch: the shared-transaction batching of
// concurrent Produce calls. The batcher logs through the client's logger.
type BatcherConfig struct {
	// MaxSize - messages sharing one batched-Produce transaction. Caps
	// lock-hold, latency tail, and the rerun cost of evicting poison.
	// Default: 100.
	MaxSize int

	// ConcurrencyLimit - workers committing a topic's batches at once
	// (one pooled connection each).
	// Default: 4.
	ConcurrencyLimit int

	// AttemptTimeout - bound on one batch transaction attempt.
	// Default: 10s.
	AttemptTimeout time.Duration

	// ShutdownGrace - how long a cancelled Produce keeps waiting for its
	// real outcome. Keep it above AttemptTimeout.
	// Default: 15s. Negative: abandon immediately.
	ShutdownGrace time.Duration
}
