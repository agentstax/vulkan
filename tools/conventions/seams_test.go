package conventions

// Holds the typed instances to the interfaces vulkan publishes for them
// (CONVENTIONS.md ## Pointers & receivers: accept interfaces only at real
// seams). A service declares its field as vulkan.Producer[Order] so its
// tests can supply their own, so a method signature that drifts off the
// instance breaks that field silently -- it breaks here first.

import (
	"testing"

	"github.com/agentstax/vulkan/pkg/vulkan"
)

// seamMessage is a payload type standing in for a user's own.
type seamMessage struct{}

func (seamMessage) SchemaVersion() int { return 1 }

func TestInstancesSatisfyPublishedSeams(t *testing.T) {
	var _ vulkan.Producer[seamMessage] = (*vulkan.ProducerInstance[seamMessage])(nil)
	var _ vulkan.Consumer[seamMessage] = (*vulkan.ConsumerInstance[seamMessage])(nil)
}
