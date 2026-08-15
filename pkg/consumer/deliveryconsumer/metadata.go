package deliveryconsumer

import (
	"context"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
)

// deliveryConsumerMetadata is the group-level config for this worker,
// written by the group's consumer declaration.
type deliveryConsumerMetadata struct {
	ClaimPollRate       time.Duration            `json:"claim_poll_rate"`
	Message             common.MessageOptions    `json:"message"`
	ConcurrencyOverride common.ConcurrencyPolicy `json:"concurrency_override"`
}

func (m *deliveryConsumerMetadata) Validate() error {
	if m.ClaimPollRate <= 0 {
		return fmt.Errorf("claim_poll_rate must be > 0, got %v", m.ClaimPollRate)
	}
	if err := m.Message.Validate(); err != nil {
		return fmt.Errorf("message: %w", err)
	}
	if err := m.ConcurrencyOverride.Validate(); err != nil {
		return fmt.Errorf("concurrency_override: %w", err)
	}
	return nil
}

// The stored message document is whatever declared the group last; clamping it
// keeps this process inside the MessageMin/MessageMax its own code sets.
func (c *DeliveryConsumerConfig) withMetadata(ctx context.Context, metadata *deliveryConsumerMetadata) *DeliveryConsumerConfig {
	applied := *c
	applied.ClaimPollRate = metadata.ClaimPollRate
	applied.ConcurrencyOverride = metadata.ConcurrencyOverride

	message := metadata.Message
	applied.Message = message.Clamp(c.MessageMin, c.MessageMax)
	if !applied.Message.Equal(&message) {
		c.Logger.WarnContext(ctx, "stored message options outside this consumer's bounds -- clamped", "stored", message, "clamped", applied.Message)
	}
	return &applied
}
