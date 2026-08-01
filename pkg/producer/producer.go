package producer

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	vulkanerrors "github.com/agentstax/vulkan/pkg/errors"
	"github.com/agentstax/vulkan/pkg/migrate"
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

	// CompactionRank - overrides arrival order when picking a CompactionKey's
	// winner: higher rank wins, equal ranks fall to the id tiebreak.
	// Default: 0 (arrival order decides).
	//
	// Rank is a COMMITMENT, not a hint: a high-rank write pins its key --
	// lower ranks lose silently until something >= it arrives.
	// Requires CompactionKey.
	// Ex: a source system's row version, a priority tier, epoch micros.
	CompactionRank int64

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

	// Message - per-message MessageOptions: what this message REQUESTS from
	// whoever consumes it (work timeout, redelivery policy, concurrency).
	// Default: nil (defaults to Producer Defaults > Consumer Defaults).
	Message *common.MessageOptions
}

// Validate rejects nonsensical option combinations.
// Mus be called after Fill().
func (o ProduceOptions) Validate() error {
	if o.CompactionRank != 0 && o.CompactionKey == "" {
		return fmt.Errorf("CompactionRank %d set without CompactionKey -- rank has nothing to rank, set CompactionKey too", o.CompactionRank)
	}
	if o.Message != nil && o.Message.Concurrency == common.ConcurrencyDefer && o.CompactionKey == "" {
		return errors.New("Concurrency 'defer' set without CompactionKey -- defer has nothing to defer on, set CompactionKey too")
	}
	if err := o.Message.Validate(); err != nil {
		return fmt.Errorf("Message: %w", err)
	}
	return nil
}

// TODO - this might need to be a common type,
// it differs from consumer.MessageRow by Message vs Payload (raw json)
type MessageRow[Message any] struct {
	Id             int64
	Message        *Message
	CreatedAt      time.Time
	RoutingKey     string
	CompactionKey  string
	CompactionRank int64
}

func NewMessageRow[Message any](id int64, message *Message, createdAt time.Time, routingKey, compactionKey string, compactionRank int64) (*MessageRow[Message], error) {
	if message == nil {
		return nil, errors.New("message must not be nil")
	}
	return &MessageRow[Message]{
		Message:        message,
		Id:             id,
		CreatedAt:      createdAt,
		RoutingKey:     routingKey,
		CompactionKey:  compactionKey,
		CompactionRank: compactionRank,
	}, nil
}

type Producer[Message any] struct {
	Topic *topic.Topic // resolved by Register from the (name, version) given to NewProducer

	topicName      string
	version        topic.SchemaVersion
	datastore      *producerDatastore[Message]
	topicDatastore *topic.TopicDatastore
	batcher        *batcher[Message]
	lifecycleCtx   context.Context
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewProducer[Message any](topicName string, version topic.SchemaVersion, ds *coredatastore.PostgresDatastore, cfg *ProducerConfig) (*Producer[Message], error) {
	if topicName == "" {
		return nil, errors.New("topic name is required")
	}
	if version < 1 {
		return nil, fmt.Errorf("SchemaVersion must be >= 1, got %d", version)
	}
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}
	if cfg == nil {
		cfg = &ProducerConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	producerDatastore, err := newProducerDatastore[Message](ds, cfg)
	if err != nil {
		return nil, err
	}
	topicDatastore, err := topic.NewTopicDatastore(ds, cfg.Retry, cfg.Logger)
	if err != nil {
		return nil, err
	}

	return &Producer[Message]{
		topicName:      topicName,
		version:        version,
		datastore:      producerDatastore,
		topicDatastore: topicDatastore,
	}, nil
}

// Register resolves this producer's topic by name against the live topic row
// and starts the producer's lifecycle.
//
// ctx must be cancellable, unless ProducerConfig.DisableGracefulShutdown
// declares otherwise.
func (p *Producer[Message]) Register(ctx context.Context) error {
	// registration is once per instance
	if p.lifecycleCtx != nil {
		if p.lifecycleCtx.Err() != nil {
			return fmt.Errorf("%w: producer for topic %q is wound down and stays down; construct a new Producer to produce again", vulkanerrors.ErrAlreadyRegistered, p.Topic.Name)
		}
		return fmt.Errorf("%w: producer for topic %q -- the context from the first Register still owns this producer's shutdown", vulkanerrors.ErrAlreadyRegistered, p.Topic.Name)
	}

	// Done() == nil -> context = Background/TODO -> no cancel can ever arrive, so the
	// shutdown phase silently wouldn't block / drain. Reject unless declared on purpose.
	if ctx.Done() == nil && !p.datastore.cfg.DisableGracefulShutdown {
		return fmt.Errorf("%w: producer for topic %q\n%s", vulkanerrors.ErrLifecycleContextNotCancellable, p.topicName, lifecycleContextHelp)
	}

	current, err := p.topicDatastore.GetTopic(ctx, p.topicName, p.version)
	if err != nil {
		return err
	}
	if current == nil {
		return fmt.Errorf("%w: topic %q version %d -- register it with MessageAdmin.RegisterTopic first", topic.ErrTopicNotFound, p.topicName, p.version)
	}
	p.Topic = current

	// fail fast if the db's schema is outside the range this build understands
	if err := migrate.AssertSchemaSupported(ctx, p.topicDatastore.Datastore.Pool, current.SystemId, current.Id); err != nil {
		return err
	}

	p.batcher = newBatcher(p.datastore, current.Id, current.PartitionSize, p.datastore.cfg)

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
func (p *Producer[Message]) Produce(ctx context.Context, message *Message, opts ProduceOptions) (*Message, error) {
	if err := p.lifecycleErr(); err != nil {
		return nil, err
	}
	opts.Message = opts.Message.Fill(p.datastore.cfg.Message)
	if err := opts.Validate(); err != nil {
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
func (p *Producer[Message]) ProduceFunc(ctx context.Context, producerFunc ProducerFunc[Message], opts ProduceOptions) (*Message, error) {
	if err := p.lifecycleErr(); err != nil {
		return nil, err
	}
	opts.Message = opts.Message.Fill(p.datastore.cfg.Message)
	if err := opts.Validate(); err != nil {
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
func (p *Producer[Message]) ProduceInTx(ctx context.Context, tx Tx, producerFunc ProducerFunc[Message], opts ProduceOptions) (*Message, error) {
	if err := p.lifecycleErr(); err != nil {
		return nil, err
	}
	opts.Message = opts.Message.Fill(p.datastore.cfg.Message)
	if err := opts.Validate(); err != nil {
		return nil, err
	}

	return p.datastore.AppendMessageInTx(ctx, tx.Raw(), p.Topic.Id, p.Topic.PartitionSize, producerFunc, opts)
}

// GetCompactionHead returns the current compaction head under compactionKey, or nil if
// nothing has been published under it.
func (p *Producer[Message]) GetCompactionHead(ctx context.Context, compactionKey string) (*MessageRow[Message], error) {
	if err := p.lifecycleErr(); err != nil {
		return nil, err
	}
	if compactionKey == "" {
		return nil, errors.New("compaction key is required")
	}
	return p.datastore.GetCompactionHead(ctx, p.Topic.Id, compactionKey)
}

// GetCompactionHeadInTx returns the current compaction head under compactionKey,
// or nil if nothing has been published under it.
// It does so within the transaction and locks the found row in a FOR UPDATE
// allowing for race-free compare and set.
func (p *Producer[Message]) GetCompactionHeadInTx(ctx context.Context, tx Tx, compactionKey string) (*MessageRow[Message], error) {
	if err := p.lifecycleErr(); err != nil {
		return nil, err
	}
	if compactionKey == "" {
		return nil, errors.New("compaction key is required")
	}
	return p.datastore.GetCompactionHeadInTx(ctx, tx.Raw(), p.Topic.Id, compactionKey)
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
func (p *Producer[Message]) lifecycleErr() error {
	if p.lifecycleCtx == nil {
		return fmt.Errorf("%w: producer for topic %q -- call Register with the application's lifetime context before producing", vulkanerrors.ErrNotRegistered, p.topicName)
	}
	if err := p.lifecycleCtx.Err(); err != nil {
		return fmt.Errorf("%w: producer for topic %q -- the lifetime context passed to Register is cancelled (%v); queued messages still commit, new ones are refused", vulkanerrors.ErrShutdownRequested, p.Topic.Name, err)
	}
	return nil
}
