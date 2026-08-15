package deliveryconsumer

import (
	"context"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	workercontroller "github.com/agentstax/vulkan/pkg/worker/controller"
)

// deliveryConsumerMetadata is the group-level config for this worker.
// Consumer declarations defines the default keys.
// Operator's who alter the group define the override keys.
type deliveryConsumerMetadata struct {
	ClaimPollRate       workercontroller.MetadataValue[time.Duration]            `json:"claim_poll_rate"`
	Message             workercontroller.MetadataValue[common.MessageOptions]    `json:"message"`
	ConcurrencyOverride workercontroller.MetadataValue[common.ConcurrencyPolicy] `json:"concurrency_override"`
}

func (m *deliveryConsumerMetadata) Validate() error {
	if m.ClaimPollRate.Effective() <= 0 {
		return fmt.Errorf("claim_poll_rate must be > 0, got %v", m.ClaimPollRate.Effective())
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
func (c *DeliveryConsumerConfig) withMetadata(ctx context.Context, metadata *deliveryConsumerMetadata) *DeliveryConsumerConfig {
	applied := *c
	applied.ClaimPollRate = metadata.ClaimPollRate.Effective()
	applied.ConcurrencyOverride = metadata.ConcurrencyOverride.Effective()

	message := metadata.Message.Effective()
	applied.Message = message.Clamp(c.MessageMin, c.MessageMax)
	if !applied.Message.Equal(&message) {
		c.Logger.WarnContext(ctx, "message override outside the group's bounds -- clamped", "override", message, "clamped", applied.Message)
	}
	return &applied
}
