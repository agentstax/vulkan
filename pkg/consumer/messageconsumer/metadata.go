package messageconsumer

import (
	"context"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	workercontroller "github.com/agentstax/vulkan/pkg/worker/controller"
)

// messageConsumerMetadata is the group-level config for this worker.
// Consumer declarations defines the default keys.
// Operator's who alter the group define the override keys.
type messageConsumerMetadata struct {
	ClaimPollRate           workercontroller.MetadataValue[time.Duration]            `json:"claim_poll_rate"`
	MaxRangeReclaims        workercontroller.MetadataValue[int]                      `json:"max_range_reclaims"`
	ExceptionInitialBackoff workercontroller.MetadataValue[time.Duration]            `json:"exception_initial_backoff"`
	Message                 workercontroller.MetadataValue[common.MessageOptions]    `json:"message"`
	ConcurrencyOverride     workercontroller.MetadataValue[common.ConcurrencyPolicy] `json:"concurrency_override"`
}

func (m *messageConsumerMetadata) Validate() error {
	if m.ClaimPollRate.Effective() <= 0 {
		return fmt.Errorf("claim_poll_rate must be > 0, got %v", m.ClaimPollRate.Effective())
	}
	if m.MaxRangeReclaims.Effective() < 1 {
		return fmt.Errorf("max_range_reclaims must be >= 1, got %d", m.MaxRangeReclaims.Effective())
	}
	if m.ExceptionInitialBackoff.Effective() <= 0 {
		return fmt.Errorf("exception_initial_backoff must be > 0, got %v", m.ExceptionInitialBackoff.Effective())
	}
	message := m.Message.Effective()
	if err := message.Validate(); err != nil {
		return fmt.Errorf("message: %w", err)
	}
	if err := m.ConcurrencyOverride.Effective().Validate(); err != nil {
		return fmt.Errorf("concurrency_override: %w", err)
	}
	return nil
}

// A message override outside the config's own MessageMin/MessageMax is
// clamped into them -- the bounds the group's code declared always hold.
func (c *MessageConsumerConfig) withMetadata(ctx context.Context, metadata *messageConsumerMetadata) *MessageConsumerConfig {
	applied := *c
	applied.ClaimPollRate = metadata.ClaimPollRate.Effective()
	applied.MaxRangeReclaims = metadata.MaxRangeReclaims.Effective()
	applied.ExceptionInitialBackoff = metadata.ExceptionInitialBackoff.Effective()
	applied.ConcurrencyOverride = metadata.ConcurrencyOverride.Effective()

	message := metadata.Message.Effective()
	applied.Message = message.Clamp(c.MessageMin, c.MessageMax)
	if !applied.Message.Equal(&message) {
		c.Logger.WarnContext(ctx, "message override outside the group's bounds -- clamped", "override", message, "clamped", applied.Message)
	}
	return &applied
}
