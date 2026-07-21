package producer

import (
	"context"
	"errors"
	"fmt"

	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/google/uuid"
)

// TODO - Consider using struct {} instead of generics

type ProducerFunc[Message any] func(ctx context.Context, tx Tx, idempotencyKey uuid.UUID) (*Message, error)
type TransactionFunc func(ctx context.Context, tx Tx) error

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
	// A hot key caps batched throughput: same-key batches commit one after another.
	// Ex: "user:123", "session:abc-def"
	CompactionKey string

	// IdempotencyKey - protects a retried AppendMessage (after a blip) from double-publishing.
	// Default: uuid.Nil (a fresh key is generated per call, protecting only
	// against retries within that one call).
	//
	// Supply your own for protection across your OWN retries too -- e.g. your
	// process crashes and restarts before learning whether a publish landed,
	// and you call Produce again with the same key. Try to use a time-ordered key
	// (UUIDv7): random (v4) keys slow throughput down considerably.
	// A caller-supplied key routes the call to a per-call transaction, never a batch.
	// Ex: a UUIDv7 persisted alongside the work before the first Produce attempt.
	IdempotencyKey uuid.UUID
}

type MessageProducer[Message any] struct {
	Topic          *topic.Topic
	datastore      *producerDatastore[Message]
	topicDatastore *topic.TopicDatastore
	batcher        *batcher[Message]
	lifecycleCtx   context.Context
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewMessageProducer[Message any](t *topic.Topic, ds *coredatastore.PostgresDatastore, cfg *MessageProducerConfig) (*MessageProducer[Message], error) {
	if t == nil {
		return nil, errors.New("topic must not be nil")
	}
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}
	if cfg == nil {
		cfg = &MessageProducerConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	producerDatastore, err := newProducerDatastore[Message](ds, cfg)
	if err != nil {
		return nil, err
	}
	topicDatastore, err := topic.NewTopicDatastore(ds, cfg.Logger, cfg.Retry)
	if err != nil {
		return nil, err
	}

	return &MessageProducer[Message]{
		Topic:          t,
		datastore:      producerDatastore,
		topicDatastore: topicDatastore,
		batcher:        newBatcher(producerDatastore, t.Id, t.PartitionSize, *cfg),
	}, nil
}

// Register validates this producer's topic handle against the live topic row
// and starts the producer's lifecycle.
func (p *MessageProducer[Message]) Register(ctx context.Context) error {
	current, err := p.topicDatastore.GetTopic(ctx, p.Topic.Name)
	if err != nil {
		return err
	}
	if current == nil {
		return fmt.Errorf("%w: topic %q (id %d) was destroyed after this handle was resolved; re-create it with topic.Register", topic.ErrTopicNotFound, p.Topic.Name, p.Topic.Id)
	}
	if current.Id != p.Topic.Id {
		return fmt.Errorf("%w: topic %q was destroyed and re-created (handle id %d, current id %d) -- this handle addresses dropped tables; resolve a fresh one with topic.Register", topic.ErrTopicStale, p.Topic.Name, p.Topic.Id, current.Id)
	}
	if *current != *p.Topic {
		return fmt.Errorf("%w: topic %q config changed after this handle was resolved (handle=%+v current=%+v); resolve a fresh one with topic.Register", topic.ErrTopicStale, p.Topic.Name, *p.Topic, *current)
	}

	// tracked for graceful shutdown draining / handling
	p.lifecycleCtx = ctx

	return nil
}

// Produce appends message to the topic, returning once it is durably
// committed. Concurrent calls share transactions: batched under load,
// committed alone (no added latency) at idle.
//
// Cancelling ctx stops the wait, not the message -- it still commits with
// its batch, so the outcome is ambiguous. To retry across that ambiguity
// (or your own crash) without double-publishing, supply an IdempotencyKey:
// the rerun dedups against whatever actually landed.
func (p *MessageProducer[Message]) Produce(ctx context.Context, message *Message, opts ProduceOptions) (*Message, error) {
	if err := p.lifecycleErr(); err != nil {
		return nil, err
	}

	// caller keys can collide -- a collision inside a shared txn stalls the
	// whole batch, so keyed calls take a per-call transaction
	if opts.IdempotencyKey != uuid.Nil {
		passthrough := func(context.Context, Tx, uuid.UUID) (*Message, error) { return message, nil }
		return p.datastore.AppendMessage(ctx, p.Topic.Id, p.Topic.PartitionSize, passthrough, opts)
	}
	return p.batcher.produce(ctx, message, opts)
}

// ProduceFunc appends the message returned by producerFunc, which runs inside
// the message's transaction -- your writes commit or roll back with it.
func (p *MessageProducer[Message]) ProduceFunc(ctx context.Context, producerFunc ProducerFunc[Message], opts ProduceOptions) (*Message, error) {
	if err := p.lifecycleErr(); err != nil {
		return nil, err
	}

	message, err := p.datastore.AppendMessage(ctx, p.Topic.Id, p.Topic.PartitionSize, producerFunc, opts)
	if err != nil {
		return nil, err
	}

	return message, nil
}

// ProduceInTx appends producerFunc's message inside a transaction the caller
// owns -- it commits or rolls back with everything else in tx.
//
// The message's IdempotencyKey stays locked until tx resolves -- any other
// call reusing that key blocks the whole time. Keep transactions that reuse
// keys short.
//
// For optimal performance call this LAST in your transaction. Producing
// effectively takes a lock on consumer progress for the whole topic: claims
// cannot advance past this message until tx commits, and every statement
// after this call extends how long that lock is held.
func (p *MessageProducer[Message]) ProduceInTx(ctx context.Context, tx Tx, producerFunc ProducerFunc[Message], opts ProduceOptions) (*Message, error) {
	if err := p.lifecycleErr(); err != nil {
		return nil, err
	}

	return p.datastore.AppendMessageInTx(ctx, tx.Raw(), p.Topic.Id, p.Topic.PartitionSize, producerFunc, opts)
}

// InTransaction opens one transaction, runs transactionFunc against it, and
// commits -- the way to publish to multiple targets atomically via ProduceInTx.
//
// It does not retry -- a transient blip or an ambiguous commit failure
// surfaces to you as-is. Wrap your own retry loop around it if you want one;
// only you know what's safe to rerun in your closure. Rerunning the whole
// closure is dedup-safe ONLY under caller-supplied IdempotencyKeys -- unset
// keys mint fresh per call, so a rerun double-publishes.
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

// lifecycleErr is the produce gate: work is only accepted between Register
// and its ctx's cancellation.
func (p *MessageProducer[Message]) lifecycleErr() error {
	if p.lifecycleCtx == nil {
		return fmt.Errorf("%w: topic %q -- call Register with the application's lifetime context before producing", ErrNotRegistered, p.Topic.Name)
	}
	if err := p.lifecycleCtx.Err(); err != nil {
		return fmt.Errorf("%w: topic %q -- the lifetime context passed to Register is cancelled (%v); queued messages still commit, new ones are refused", ErrShutdownRequested, p.Topic.Name, err)
	}
	return nil
}
