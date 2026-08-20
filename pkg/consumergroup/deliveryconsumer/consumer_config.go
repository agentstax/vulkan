package deliveryconsumer

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/common/logging"
	"github.com/agentstax/vulkan/pkg/consumergroup"
)

// DeliveryConsumerConfig is the slice of the group's consumer config this
// worker row runs on. pkg/consumer resolves and validates the whole config once,
// then builds this -- so WithDefaults here only backstops a direct caller.
type DeliveryConsumerConfig struct {
	BatchLimit       int // delivery rows claimed per poll
	FanOutBatchLimit int // max log rows the fan-out scans per tick -- bounds a cold group's catch-up scan
	ClaimPollRate    time.Duration
	TimeoutGrace     time.Duration // scheduling slack for a consumerFunc that DID respect ctx.Done() to unwind before the hard cutoff abandons it

	InstanceTTL time.Duration // how long a claimed worker_instance row stays live without a heartbeat renewal

	// Message / MessageMin / MessageMax / ConcurrencyOverride resolve each
	// message's own requested options against this group's defaults and bounds.
	Message             *common.MessageOptions
	MessageMin          *common.MessageOptions
	MessageMax          *common.MessageOptions
	ConcurrencyOverride common.ConcurrencyPolicy

	Logger logging.Logger
	Retry  *common.RetryPolicy // transient-error retry policy for this worker's own Postgres calls
}

func (c *DeliveryConsumerConfig) WithDefaults() *DeliveryConsumerConfig {
	if c.BatchLimit == 0 {
		c.BatchLimit = 1
	}
	if c.FanOutBatchLimit == 0 {
		c.FanOutBatchLimit = 1000 // fanout rows are cheap next to processing -- a wide default so only genuinely cold groups feel the cap
	}
	if c.ClaimPollRate == 0 {
		c.ClaimPollRate = 5 * time.Second
	}
	if c.TimeoutGrace == 0 {
		c.TimeoutGrace = 100 * time.Millisecond
	}
	if c.InstanceTTL == 0 {
		c.InstanceTTL = 30 * time.Second
	}

	c.Message = c.Message.WithDefaults()

	bounds := *c.Message
	bounds.Concurrency = ""
	c.MessageMax = c.MessageMax.Fill(&bounds)

	c.Retry = c.Retry.WithDefaults()
	if c.Logger == nil {
		c.Logger = logging.NewDefaultLogger(os.Stderr)
	}
	c.Logger = logging.BufferLogger(c.Logger)
	return c
}

// Validate runs after WithDefaults -- anything still out of range here was
// set by the caller, not left unset.
func (c *DeliveryConsumerConfig) Validate() error {
	if c.BatchLimit < 1 {
		return fmt.Errorf("BatchLimit must be >= 1, got %d", c.BatchLimit)
	}
	if c.FanOutBatchLimit < 1 {
		return fmt.Errorf("FanOutBatchLimit must be >= 1, got %d", c.FanOutBatchLimit)
	}
	if c.ClaimPollRate <= 0 {
		return fmt.Errorf("ClaimPollRate must be > 0, got %v", c.ClaimPollRate)
	}
	if c.TimeoutGrace <= 0 {
		return fmt.Errorf("TimeoutGrace must be > 0, got %v", c.TimeoutGrace)
	}
	if c.InstanceTTL <= 0 {
		return fmt.Errorf("InstanceTTL must be > 0, got %v", c.InstanceTTL)
	}
	if err := c.Message.Validate(); err != nil {
		return fmt.Errorf("Message: %w", err)
	}
	if err := c.MessageMax.Validate(); err != nil {
		return fmt.Errorf("MessageMax: %w", err)
	}
	if err := c.Retry.Validate(); err != nil {
		return fmt.Errorf("Retry: %w", err)
	}
	return nil
}

func (c *DeliveryConsumerConfig) resolveMessageOptions(requested *common.MessageOptions) *common.MessageOptions {
	return requested.Fill(c.Message).Clamp(c.MessageMin, c.MessageMax).ResolveConcurrency(c.ConcurrencyOverride)
}

// withMetadata resolves what this run uses: the stored config, with its message
// options clamped. The stored options are whatever declared the group last, so
// the clamp is what keeps this process inside the MessageMin/MessageMax its own
// code sets.
func (c *DeliveryConsumerConfig) withMetadata(ctx context.Context, metadata *deliveryConsumerMetadata) *DeliveryConsumerConfig {
	applied := *c
	applied.ClaimPollRate = metadata.ClaimPollRate
	applied.ConcurrencyOverride = metadata.ConcurrencyOverride

	message := metadata.Message
	applied.Message = message.Clamp(c.MessageMin, c.MessageMax)
	if !applied.Message.Equal(&message) {
		c.Logger.WarnContext(ctx, consumergroup.EventStoredOptionsClamped.Message, "code", consumergroup.EventStoredOptionsClamped.Code, "stored", message, "clamped", applied.Message)
	}
	return &applied
}
