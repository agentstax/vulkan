package producer

import (
	"context"

	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// TODO - Consider using struct {} instead of generics

// TODO - the pgx.Tx here couples this datastore-agnostic package to pgx should try to decouple if we ever want many datastores (probably not)

type ProducerFunc[WorkType any] func(ctx context.Context, tx pgx.Tx, idempotencyKey uuid.UUID) (*WorkType, error)

type Producer[WorkType any] interface {
	Produce(ctx context.Context, work *WorkType) error
}

// ProduceOptions holds per-message knobs that are optional and rarely set --
// the zero value means "neither is set," so a caller who doesn't need them
// never has to name them.
type ProduceOptions struct {
	// RoutingKey - matched against a consumer group's bindings to decide
	// whether that group receives this message at all.
	//
	// Leave unset for messages every consumer group should see regardless of
	// binding. "" is stored as no routing key, not an empty-string match.
	// Ex: "orders.created", "billing.invoice.paid"
	RoutingKey string

	// CompactionKey - identifies this message as one version of a key whose
	// claims should only ever return the latest version, not every version
	// ever written.
	//
	// Leave unset for messages that aren't part of a compacted stream --
	// every message with "" is delivered independently, never superseded.
	// Ex: "user:123", "session:abc-def"
	CompactionKey string

	// IdempotencyKey protects a retried AppendMessage (after a blip) from
	// double-publishing. Supply your own if you need protection across your
	// OWN retries too -- e.g. your process crashes and restarts before
	// learning whether a publish landed, and you call Produce again with the
	// same key. Leave unset (uuid.Nil) and one is generated fresh per
	// AppendMessage call, protecting only against retries within that one call.
	IdempotencyKey uuid.UUID
}

type WorkProducer[WorkType any] struct {
	Topic     *topic.Topic // the resolved topic.Register return -- id already looked up, never re-resolved per message
	datastore Datastore[WorkType]
}

func NewWorkProducer[WorkType any](t *topic.Topic, datastore Datastore[WorkType]) *WorkProducer[WorkType] {
	return &WorkProducer[WorkType]{
		Topic:     t,
		datastore: datastore,
	}
}

func (p *WorkProducer[WorkType]) Produce(ctx context.Context, producerFunc ProducerFunc[WorkType], opts ProduceOptions) (*WorkType, error) {
	message, err := p.datastore.AppendMessage(ctx, p.Topic.Id, p.Topic.PartitionSize, producerFunc, opts)
	if err != nil {
		return nil, err
	}

	return message, nil
}
