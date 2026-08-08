package producer

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/topic"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
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
	datastore       *producerDatastore[Message]
	topicController *topiccontroller.TopicController
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewProducer[Message any](ds *coredatastore.PostgresDatastore, cfg *ProducerConfig) (*Producer[Message], error) {
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
	topicController, err := topiccontroller.NewTopicController(ds, &topiccontroller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &Producer[Message]{
		datastore:       producerDatastore,
		topicController: topicController,
	}, nil
}

// Register resolves the named topic against the live topic row and returns an
// instance that produces to it. Callable many times -- each call returns an
// independent instance. ctx bounds only this call's I/O.
func (p *Producer[Message]) Register(ctx context.Context, topicName string, version topic.SchemaVersion) (*ProducerInstance[Message], error) {
	if topicName == "" {
		return nil, errors.New("topic name is required")
	}
	if version < 1 {
		return nil, fmt.Errorf("SchemaVersion must be >= 1, got %d", version)
	}

	current, err := p.topicController.GetTopic(ctx, topicName, version)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, fmt.Errorf("%w: topic %q version %d -- register it with MessageAdmin.RegisterTopic first", topic.ErrTopicNotFound, topicName, version)
	}

	// fail fast if the db's schema is outside the range this build understands
	if err := p.topicController.AssertSchemaSupported(ctx, current.SystemId, current.Id); err != nil {
		return nil, err
	}

	return NewProducerInstance(current, p.datastore)
}
