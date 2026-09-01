package vulkan

import (
	"context"
)

// Producer is every verb *ProducerInstance[Message] publishes, named so a
// service can hold the interface and a test can supply its own.
type Producer[Message Versioned] interface {
	Produce(ctx context.Context, message *Message, options *ProduceOptions) (*ProduceResult[Message], error)
	ProduceBatch(ctx context.Context, items ...*ProduceItem[Message]) ([]*ProduceResult[Message], error)
	ProduceFunc(ctx context.Context, producerFunc ProducerFunc[Message], options *ProduceOptions) (*ProduceResult[Message], error)
	ProduceInTx(ctx context.Context, tx Tx, producerFunc ProducerFunc[Message], options *ProduceOptions) (*ProduceResult[Message], error)
	GetCompactionHeadInTx(ctx context.Context, tx Tx, messageKey string) (*MessageData[Message], error)
}
