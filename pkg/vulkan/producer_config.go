package vulkan

import (
	"time"
)

// ProducerConfig is the client's producer declaration: this process's
// message defaults and batching. Ambient (logger, retry) lives on
// ClientConfig; defaults fill and validation runs when the client builds
// its producer.
type ProducerConfig struct {
	// Message - this producer's default MessageOptions, merged UNDER every
	// produce: a field the per-produce ProduceOptions.Message leaves unset
	// takes its value from here before the message is stored. Fields unset in
	// both stay unset -- the consumer decides.
	// Default: nil (no producer-side defaults).
	Message *MessageOptions

	// Batch - knobs for the shared-transaction batching of concurrent Produce
	// calls.
	Batch BatcherConfig

	// SlowProduceThreshold - a produce call running longer than this logs a
	// warn line with its duration. ProduceFunc and ProduceFuncInTx include
	// the caller's closure; both InTx verbs end at the insert, before the
	// caller's commit.
	// Default: 0 (disabled).
	SlowProduceThreshold time.Duration
}
