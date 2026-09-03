package messageconsumer

import (
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
)

// MessageConsumerMetadata is the group config stored on the message consumer
// worker row: only the fields the declaration set. Unset fields resolve to
// the library defaults when the row is read back.
type MessageConsumerMetadata struct {
	Message                 *common.MessageOptions   `json:"message,omitempty"`
	MessageMin              *common.MessageOptions   `json:"message_min,omitempty"`
	MessageMax              *common.MessageOptions   `json:"message_max,omitempty"`
	ConcurrencyOverride     common.ConcurrencyPolicy `json:"concurrency_override,omitempty"`
	ExceptionInitialBackoff time.Duration            `json:"exception_initial_backoff,omitempty"`
	MaxRangeReclaims        int                      `json:"max_range_reclaims,omitempty"`
}

// Validate accepts the sparse document -- zero means unset -- and rejects
// only values no declaration could have written.
func (m *MessageConsumerMetadata) Validate() error {
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
	if m.ExceptionInitialBackoff < 0 {
		return fmt.Errorf("exception_initial_backoff must be >= 0, got %v", m.ExceptionInitialBackoff)
	}
	if m.MaxRangeReclaims < 0 {
		return fmt.Errorf("max_range_reclaims must be >= 0, got %d", m.MaxRangeReclaims)
	}
	return nil
}

// Equal must compare every field the struct declares -- one left out here
// silently stops refreshing, keeping its old value on every running instance.
func (m *MessageConsumerMetadata) Equal(other *MessageConsumerMetadata) bool {
	if m == nil || other == nil {
		return m == other
	}
	return m.Message.Equal(other.Message) &&
		m.MessageMin.Equal(other.MessageMin) &&
		m.MessageMax.Equal(other.MessageMax) &&
		m.ConcurrencyOverride == other.ConcurrencyOverride &&
		m.ExceptionInitialBackoff == other.ExceptionInitialBackoff &&
		m.MaxRangeReclaims == other.MaxRangeReclaims
}
