package producer

import (
	"errors"
	"github.com/agentstax/vulkan/pkg/topic"
)

// ProduceItem is one message plus its options -- the unit ProduceBatch takes.
type ProduceItem[Message topic.Versioned] struct {
	Message *Message
	Options ProduceOptions
}

// A set Options.IdempotencyKey is rejected: one hot key would stall the
// batch's whole shared transaction, so keyed messages go through Produce.
// options may be nil for the defaults.
func NewProduceItem[Message topic.Versioned](message *Message, options *ProduceOptions) (*ProduceItem[Message], error) {
	if message == nil {
		return nil, errors.New("message must not be nil")
	}

	resolved := ProduceOptions{}
	if options != nil {
		resolved = *options
	}
	if resolved.IdempotencyKey != "" {
		return nil, errors.New("IdempotencyKey is not supported in a batch -- produce keyed messages individually")
	}

	return &ProduceItem[Message]{
		Message: message,
		Options: resolved,
	}, nil
}
