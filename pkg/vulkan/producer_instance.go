package vulkan

import (
	"context"
	"errors"

	"github.com/agentstax/vulkan/pkg/producer"
)

// ProducerInstance is a registered producer: it appends messages to the
// topic RegisterProducer resolved. Every verb is producer.ProducerInstance's,
// which holds each one's contract.
type ProducerInstance[Message Versioned] struct {
	instance *producer.ProducerInstance[Message]
}

func newProducerInstance[Message Versioned](instance *producer.ProducerInstance[Message]) (*ProducerInstance[Message], error) {
	if instance == nil {
		return nil, errors.New("instance must not be nil")
	}
	return &ProducerInstance[Message]{instance: instance}, nil
}

// Produce appends message, returning once it is durably committed.
func (p *ProducerInstance[Message]) Produce(ctx context.Context, message *Message, options *ProduceOptions) (*ProduceResult[Message], error) {
	return p.instance.Produce(ctx, message, options)
}

// ProduceBatch appends every item in one transaction -- none land unless all do.
func (p *ProducerInstance[Message]) ProduceBatch(ctx context.Context, items ...*ProduceItem[Message]) ([]*ProduceResult[Message], error) {
	return p.instance.ProduceBatch(ctx, items...)
}

// NewProduceItem pairs one message with its options for ProduceBatch.
// options may be nil for the defaults.
func NewProduceItem[Message Versioned](message *Message, options *ProduceOptions) (*ProduceItem[Message], error) {
	return producer.NewProduceItem(message, options)
}

// ProduceFunc appends the message producerFunc returns from inside the
// message's own transaction.
func (p *ProducerInstance[Message]) ProduceFunc(ctx context.Context, producerFunc ProducerFunc[Message], options *ProduceOptions) (*ProduceResult[Message], error) {
	return p.instance.ProduceFunc(ctx, producerFunc, options)
}

// ProduceInTx appends message inside a transaction the caller owns.
func (p *ProducerInstance[Message]) ProduceInTx(ctx context.Context, tx Tx, message *Message, options *ProduceOptions) (*ProduceResult[Message], error) {
	return p.instance.ProduceInTx(ctx, tx, message, options)
}

// ProduceFuncInTx appends the message producerFunc returns, inside a
// transaction the caller owns.
func (p *ProducerInstance[Message]) ProduceFuncInTx(ctx context.Context, tx Tx, producerFunc ProducerFunc[Message], options *ProduceOptions) (*ProduceResult[Message], error) {
	return p.instance.ProduceFuncInTx(ctx, tx, producerFunc, options)
}

// GetCompactionHeadInTx reads messageKey's compaction head under tx, locking
// the row FOR UPDATE; nil when nothing has been produced under it.
func (p *ProducerInstance[Message]) GetCompactionHeadInTx(ctx context.Context, tx Tx, messageKey string) (*StoredMessage[Message], error) {
	return p.instance.GetCompactionHeadInTx(ctx, tx, messageKey)
}
