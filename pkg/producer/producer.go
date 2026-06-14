package producer

import "context"

// use struct{} to force serializeable object for jsonb field

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

func (p *WorkProducer[WorkType]) Produce(ctx context.Context, work *WorkType) error {
	err := p.datastore.AppendMessage(ctx, work)
	if err != nil {
		return err
	}

	return nil
}
