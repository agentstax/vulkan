package producer

import (
	"context"

	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/google/uuid"
)

// TODO - Consider using struct {} instead of generics

type ProducerFunc[Message any] func(ctx context.Context, tx Tx, idempotencyKey uuid.UUID) (*Message, error)
type TransactionFunc func(ctx context.Context, tx Tx) error

type Producer[Message any] interface {
	Produce(ctx context.Context, work *Message) error
}

// ProduceOptions holds per-message knobs that are optional and rarely set --
// the zero value means "neither is set," so a caller who doesn't need them
// never has to name them.
type ProduceOptions struct {
	// RoutingKey - matched against a consumer group's bindings to decide
	// whether that group receives this message at all.
	// Default: "" (no routing key; every group receives it regardless of binding).
	//
	// "" is stored as no routing key, not an empty-string match.
	// Ex: "orders.created", "billing.invoice.paid"
	RoutingKey string

	// CompactionKey - identifies this message as one version of a key whose
	// claims should only ever return the latest version, not every version ever written.
	// Default: "" (not part of a compacted stream; delivered independently, never superseded).
	//
	// Set it to opt this message into log compaction under that key.
	// Ex: "user:123", "session:abc-def"
	CompactionKey string

	// IdempotencyKey - protects a retried AppendMessage (after a blip) from double-publishing.
	// Default: uuid.Nil (a fresh key is generated per call, protecting only
	// against retries within that one call).
	//
	// Supply your own for protection across your OWN retries too -- e.g. your
	// process crashes and restarts before learning whether a publish landed,
	// and you call Produce again with the same key. Prefer a time ordered key
	// like UUIDv7 such that data is logically ordered on disk for better efficency.
	// Ex: a UUIDv7 persisted alongside the work before the first Produce attempt.
	IdempotencyKey uuid.UUID
}

type MessageProducer[Message any] struct {
	Topic     *topic.Topic // the resolved topic.Register return -- id already looked up, never re-resolved per message
	datastore Datastore[Message]
}

func NewMessageProducer[Message any](t *topic.Topic, datastore Datastore[Message]) *MessageProducer[Message] {
	return &MessageProducer[Message]{
		Topic:     t,
		datastore: datastore,
	}
}

// TODO - make good doc comments
func (p *MessageProducer[Message]) Produce(ctx context.Context, producerFunc ProducerFunc[Message], opts ProduceOptions) (*Message, error) {
	message, err := p.datastore.AppendMessage(ctx, p.Topic.Id, p.Topic.PartitionSize, producerFunc, opts)
	if err != nil {
		return nil, err
	}

	return message, nil
}

// TODO - make good doc comments
func (p *MessageProducer[Message]) ProduceInTx(ctx context.Context, tx Tx, producerFunc ProducerFunc[Message], opts ProduceOptions) (*Message, error) {
	return p.datastore.AppendMessageInTx(ctx, tx.Raw(), p.Topic.Id, p.Topic.PartitionSize, producerFunc, opts)
}

// TODO - make good doc comments
// InTransaction does not retry -- a transient blip or an ambiguous commit
// failure surfaces to you as-is. Wrap your own retry loop around it if you
// want one; only you know what's safe to rerun in your closure.
func InTransaction(ctx context.Context, ds *coredatastore.PostgresDatastore, transactionFunc TransactionFunc) error {
	tx, err := ds.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if err := transactionFunc(ctx, newVulkanTx(tx)); err != nil {
		return err
	}

	return tx.Commit(ctx)
}
