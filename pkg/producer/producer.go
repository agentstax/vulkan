package producer

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// TODO - Consider using struct {} instead of generics

// TODO - the pgx.Tx here couples this datastore-agnostic package to pgx should try to decouple if we ever want many datastores (probably not)
type ProducerFunc[WorkType any] func(ctx context.Context, tx pgx.Tx) (*WorkType, error)

type Producer[WorkType any] interface {
	Produce(ctx context.Context, work *WorkType) error
}

type WorkProducer[WorkType any] struct {
	datastore Datastore[WorkType]
}

func NewWorkProducer[WorkType any](datastore Datastore[WorkType]) *WorkProducer[WorkType] {
	return &WorkProducer[WorkType]{
		datastore: datastore,
	}
}

func (p *WorkProducer[WorkType]) Produce(ctx context.Context, producerFunc ProducerFunc[WorkType], routingKey string) (*WorkType, error) {
	message, err := p.datastore.AppendMessage(ctx, producerFunc, routingKey)
	if err != nil {
		return nil, err
	}

	return message, nil
}
