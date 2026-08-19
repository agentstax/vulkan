package exceptionconsumer

import (
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
)

// exceptionConsumerMetadata is the config stored on the exception consumer
// worker row.
type exceptionConsumerMetadata struct {
	ClaimPollRate       time.Duration            `json:"claim_poll_rate"`
	Message             common.MessageOptions    `json:"message"`
	ConcurrencyOverride common.ConcurrencyPolicy `json:"concurrency_override"`
}

func (m *exceptionConsumerMetadata) Validate() error {
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
