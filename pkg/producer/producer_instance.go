package producer

import (
	"context"
	"errors"

	"github.com/agentstax/vulkan/pkg/producer/batcher"
	"github.com/agentstax/vulkan/pkg/producer/controller"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/google/uuid"
)

// ProducerInstance is a registered producer: it appends messages to the topic
// Register resolved. Shutdown is per call -- a cancelled ctx refuses that
// call's message, the instance itself never stops accepting work.
type ProducerInstance[Message any] struct {
	Topic *topic.Topic

	controller *controller.ProducerController[Message]
	batcher    *batcher.Batcher[Message]
	cfg        ProducerConfig // value copy, resolved+validated -- caller mutations after construction change nothing
}

// cfg is already resolved (WithDefaults + Validate) by NewProducer.
func NewProducerInstance[Message any](resolvedTopic *topic.Topic, producerController *controller.ProducerController[Message], cfg ProducerConfig) (*ProducerInstance[Message], error) {
	if resolvedTopic == nil {
		return nil, errors.New("topic must not be nil")
	}
	if producerController == nil {
		return nil, errors.New("controller must not be nil")
	}

	topicBatcher, err := batcher.NewBatcher(producerController, resolvedTopic.Id, resolvedTopic.PartitionSize, &batcher.BatcherConfig{
		Logger:           cfg.Logger,
		MaxSize:          cfg.BatchMaxSize,
		ConcurrencyLimit: cfg.BatchConcurrencyLimit,
		AttemptTimeout:   cfg.BatchAttemptTimeout,
		ShutdownGrace:    cfg.BatchShutdownGrace,
	})
	if err != nil {
		return nil, err
	}

	return &ProducerInstance[Message]{
		Topic:      resolvedTopic,
		controller: producerController,
		batcher:    topicBatcher,
		cfg:        cfg,
	}, nil
}

// Produce appends message to the topic, returning once it is durably
// committed. Concurrent calls share transactions: batched under load,
// committed alone (no added latency) at idle.
//
// Cancelling ctx stops the wait, not the message -- it still commits with
// its batch, so the outcome is ambiguous. To retry across that ambiguity
// (or your own crash) without double-publishing, supply an IdempotencyKey:
// the rerun dedups against whatever actually landed, reported as
// ProduceResult.Duplicate == true.
func (p *ProducerInstance[Message]) Produce(ctx context.Context, message *Message, opts ProduceOptions) (*ProduceResult[Message], error) {
	opts.Message = opts.Message.Fill(p.cfg.Message)
	if err := opts.Validate(); err != nil {
		return nil, err
	}

	// caller keys can collide -- a collision inside a shared txn stalls the
	// whole batch, so keyed calls take a per-call transaction
	if opts.IdempotencyKey != uuid.Nil {
		passthrough := func(context.Context, Tx, uuid.UUID) (*Message, error) { return message, nil }
		appended, err := p.controller.AppendMessage(ctx, p.Topic.Id, p.Topic.PartitionSize, passthrough, opts)
		if err != nil {
			return nil, err
		}
		return NewProduceResult(appended.Message, appended.Id, appended.Duplicate)
	}

	appended, err := p.batcher.Produce(ctx, message, opts)
	if err != nil {
		return nil, err
	}
	return NewProduceResult(appended.Message, appended.Id, appended.Duplicate)
}

// ProduceFunc appends the message returned by producerFunc, which runs inside
// the message's transaction -- your writes commit or roll back with it.
func (p *ProducerInstance[Message]) ProduceFunc(ctx context.Context, producerFunc ProducerFunc[Message], opts ProduceOptions) (*ProduceResult[Message], error) {
	opts.Message = opts.Message.Fill(p.cfg.Message)
	if err := opts.Validate(); err != nil {
		return nil, err
	}

	appended, err := p.controller.AppendMessage(ctx, p.Topic.Id, p.Topic.PartitionSize, producerFunc, opts)
	if err != nil {
		return nil, err
	}
	return NewProduceResult(appended.Message, appended.Id, appended.Duplicate)
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
func (p *ProducerInstance[Message]) ProduceInTx(ctx context.Context, tx Tx, producerFunc ProducerFunc[Message], opts ProduceOptions) (*ProduceResult[Message], error) {
	opts.Message = opts.Message.Fill(p.cfg.Message)
	if err := opts.Validate(); err != nil {
		return nil, err
	}

	appended, err := p.controller.AppendMessageInTx(ctx, tx, p.Topic.Id, p.Topic.PartitionSize, producerFunc, opts)
	if err != nil {
		return nil, err
	}
	return NewProduceResult(appended.Message, appended.Id, appended.Duplicate)
}

// GetCompactionHead returns the current compaction head under compactionKey, or nil if
// nothing has been published under it.
func (p *ProducerInstance[Message]) GetCompactionHead(ctx context.Context, compactionKey string) (*MessageRow[Message], error) {
	return p.controller.GetCompactionHead(ctx, p.Topic.Id, compactionKey)
}

// GetCompactionHeadInTx returns the current compaction head under compactionKey,
// or nil if nothing has been published under it.
// It does so within the transaction and locks the found row in a FOR UPDATE
// allowing for race-free compare and set.
func (p *ProducerInstance[Message]) GetCompactionHeadInTx(ctx context.Context, tx Tx, compactionKey string) (*MessageRow[Message], error) {
	return p.controller.GetCompactionHeadInTx(ctx, tx, p.Topic.Id, compactionKey)
}
