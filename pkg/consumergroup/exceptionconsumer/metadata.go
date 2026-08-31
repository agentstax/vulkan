package exceptionconsumer

import (
	"fmt"

	"github.com/agentstax/vulkan/pkg/common"
)

// ExceptionConsumerMetadata is the group config stored on the exception
// consumer worker row: only the fields the declaration set. Unset fields
// resolve to the library defaults when the row is read back.
type ExceptionConsumerMetadata struct {
	Message             *common.MessageOptions   `json:"message,omitempty"`
	MessageMin          *common.MessageOptions   `json:"message_min,omitempty"`
	MessageMax          *common.MessageOptions   `json:"message_max,omitempty"`
	ConcurrencyOverride common.ConcurrencyPolicy `json:"concurrency_override,omitempty"`
}

// Validate accepts the sparse document -- zero means unset -- and rejects
// only values no declaration could have written.
func (m *ExceptionConsumerMetadata) Validate() error {
	if err := m.Message.Validate(); err != nil {
		return fmt.Errorf("message: %w", err)
	}
	if err := m.MessageMin.Validate(); err != nil {
		return fmt.Errorf("message_min: %w", err)
	}
	if err := m.MessageMax.Validate(); err != nil {
		return fmt.Errorf("message_max: %w", err)
	}
	if err := m.ConcurrencyOverride.Validate(); err != nil {
		return fmt.Errorf("concurrency_override: %w", err)
	}
	return nil
}

// Equal must compare every field the struct declares -- one left out here
// silently stops refreshing, keeping its old value on every running instance.
func (m *ExceptionConsumerMetadata) Equal(other *ExceptionConsumerMetadata) bool {
	if m == nil || other == nil {
		return m == other
	}
	return m.Message.Equal(other.Message) &&
		m.MessageMin.Equal(other.MessageMin) &&
		m.MessageMax.Equal(other.MessageMax) &&
		m.ConcurrencyOverride == other.ConcurrencyOverride
}
