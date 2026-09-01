package vulkan

import (
	"context"
)

// Consumer is every verb *ConsumerInstance[Message] publishes, named so a
// service can hold the interface and a test can supply its own.
type Consumer[Message Versioned] interface {
	Consume(ctx context.Context, consumerFunc ConsumerFunc[Message], options *ConsumeOptions) error
}
