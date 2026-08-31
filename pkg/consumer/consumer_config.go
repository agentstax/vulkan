package consumer

import (
	"fmt"
	"os"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/common/logging"
	"github.com/agentstax/vulkan/pkg/consumergroup"
)

// ConsumerConfig is the group's declaration: what the group means, identical
// for every instance of the group. Session settings -- how one process runs
// -- live on ConsumeOptions at Consume.
type ConsumerConfig struct {
	// Message - default MessageOptions: fills any option the produced message left unset.
	// Default: Timeout 30s; Retry MaxRetries 3 with the default curve.
	Message *common.MessageOptions

	// MessageMin - per-option floors: raises any resolved option below these.
	// Concurrency is not orderable and must stay unset.
	// Default: nil (no floors).
	MessageMin *common.MessageOptions

	// MessageMax - per-option ceilings: lowers any resolved option above these.
	// Concurrency is not orderable and must stay unset.
	// Default: Message's values -- messages cannot request above the group's
	// defaults unless raised here.
	MessageMax *common.MessageOptions

	// ConcurrencyOverride - this group runs every message under this policy,
	// beating whatever the message requested.
	// Default: "" (honor each message's own policy).
	ConcurrencyOverride common.ConcurrencyPolicy

	// Start - where a group's cursor is placed when Register creates it;
	// a group that already has a cursor row keeps its position.
	// Default: consumergroup.Beginning() -- the oldest retained message.
	Start consumergroup.CursorPosition

	ExceptionInitialBackoff time.Duration // can_run_after delay when an exception/terminal row is first written (Commit/PartialCommit) -- Message.Retry takes over on later retries
	MaxRangeReclaims        int           // past this many reclaims a range is POISON -- quarantined into the exception window instead of handed out again

	Logger logging.Logger      // pass your own *slog.Logger or anything satisfying logging.Logger. Default: text lines to stderr, warn level and up.
	Retry  *common.RetryPolicy // transient-error retry policy for this consumer's own Postgres calls -- never applies to message redelivery, that is Message.Retry. Default: common.NewDefaultRetryPolicy().
}

func (c *ConsumerConfig) WithDefaults() *ConsumerConfig {
	c.Message = c.Message.WithDefaults()

	// ceilings must always exist -- lease sizing and the kill backstop need them
	bounds := *c.Message
	bounds.Concurrency = "" // should never be set for a 'bound'
	c.MessageMax = c.MessageMax.Fill(&bounds)

	if c.ExceptionInitialBackoff == 0 {
		c.ExceptionInitialBackoff = 5 * time.Second
	}

	if c.MaxRangeReclaims == 0 {
		c.MaxRangeReclaims = 3
	}

	if c.Retry == nil {
		c.Retry = &common.RetryPolicy{}
	}
	c.Retry = c.Retry.WithDefaults()

	if c.Logger == nil {
		c.Logger = logging.NewDefaultLogger(os.Stderr)
	}
	c.Logger = logging.NewPipelineLogger(c.Logger, &logging.PipelineLoggerConfig{Buffer: true})

	return c
}

// Validate runs after WithDefaults -- anything still out of range here was
// set by the caller, not left unset.
func (c *ConsumerConfig) Validate() error {
	// Message.Timeout <= 0 degenerates the lease window math
	if c.Message.Timeout <= 0 {
		return fmt.Errorf("Message.Timeout must be > 0, got %v", c.Message.Timeout)
	}
	if err := c.Message.Retry.Validate(); err != nil {
		return fmt.Errorf("Message.Retry: %w", err)
	}

	if err := c.MessageMin.Validate(); err != nil {
		return fmt.Errorf("MessageMin: %w", err)
	}
	if err := c.MessageMax.Validate(); err != nil {
		return fmt.Errorf("MessageMax: %w", err)
	}
	if c.MessageMin != nil && c.MessageMin.Concurrency != "" {
		return fmt.Errorf("MessageMin must not set Concurrency -- a policy is not orderable, got %q", c.MessageMin.Concurrency)
	}
	if c.MessageMax.Concurrency != "" {
		return fmt.Errorf("MessageMax must not set Concurrency -- a policy is not orderable, got %q", c.MessageMax.Concurrency)
	}
	if err := c.ConcurrencyOverride.Validate(); err != nil {
		return fmt.Errorf("ConcurrencyOverride: %w", err)
	}
	if err := c.Start.Kind.Validate(); err != nil {
		return fmt.Errorf("Start.Kind: %w", err)
	}

	if c.ExceptionInitialBackoff <= 0 {
		return fmt.Errorf("ExceptionInitialBackoff must be > 0, got %v", c.ExceptionInitialBackoff)
	}
	if c.MaxRangeReclaims < 1 {
		return fmt.Errorf("MaxRangeReclaims must be >= 1, got %d", c.MaxRangeReclaims)
	}

	if err := c.Retry.Validate(); err != nil {
		return fmt.Errorf("Retry: %w", err)
	}
	return c.validateMessageBounds()
}

// enforces MessageMin <= Message <= MessageMax per field, so every resolved
// option lands inside the bounds
func (c *ConsumerConfig) validateMessageBounds() error {
	if c.MessageMin != nil {
		if c.MessageMin.Timeout > c.Message.Timeout {
			return fmt.Errorf("MessageMin.Timeout (%v) must be <= Message.Timeout (%v)", c.MessageMin.Timeout, c.Message.Timeout)
		}
		if r := c.MessageMin.Retry; r != nil {
			if r.MaxRetries > c.Message.Retry.MaxRetries {
				return fmt.Errorf("MessageMin.Retry.MaxRetries (%d) must be <= Message.Retry.MaxRetries (%d)", r.MaxRetries, c.Message.Retry.MaxRetries)
			}
			if r.BaseDelay > c.Message.Retry.BaseDelay {
				return fmt.Errorf("MessageMin.Retry.BaseDelay (%v) must be <= Message.Retry.BaseDelay (%v)", r.BaseDelay, c.Message.Retry.BaseDelay)
			}
			if r.MaxDelay > c.Message.Retry.MaxDelay {
				return fmt.Errorf("MessageMin.Retry.MaxDelay (%v) must be <= Message.Retry.MaxDelay (%v)", r.MaxDelay, c.Message.Retry.MaxDelay)
			}
			if r.Exponent > c.Message.Retry.Exponent {
				return fmt.Errorf("MessageMin.Retry.Exponent (%d) must be <= Message.Retry.Exponent (%d)", r.Exponent, c.Message.Retry.Exponent)
			}
		}
	}
	if c.Message.Timeout > c.MessageMax.Timeout {
		return fmt.Errorf("Message.Timeout (%v) must be <= MessageMax.Timeout (%v)", c.Message.Timeout, c.MessageMax.Timeout)
	}
	if c.Message.Retry.MaxRetries > c.MessageMax.Retry.MaxRetries {
		return fmt.Errorf("Message.Retry.MaxRetries (%d) must be <= MessageMax.Retry.MaxRetries (%d)", c.Message.Retry.MaxRetries, c.MessageMax.Retry.MaxRetries)
	}
	if c.Message.Retry.BaseDelay > c.MessageMax.Retry.BaseDelay {
		return fmt.Errorf("Message.Retry.BaseDelay (%v) must be <= MessageMax.Retry.BaseDelay (%v)", c.Message.Retry.BaseDelay, c.MessageMax.Retry.BaseDelay)
	}
	if c.Message.Retry.MaxDelay > c.MessageMax.Retry.MaxDelay {
		return fmt.Errorf("Message.Retry.MaxDelay (%v) must be <= MessageMax.Retry.MaxDelay (%v)", c.Message.Retry.MaxDelay, c.MessageMax.Retry.MaxDelay)
	}
	if c.Message.Retry.Exponent > c.MessageMax.Retry.Exponent {
		return fmt.Errorf("Message.Retry.Exponent (%d) must be <= MessageMax.Retry.Exponent (%d)", c.Message.Retry.Exponent, c.MessageMax.Retry.Exponent)
	}
	return nil
}
