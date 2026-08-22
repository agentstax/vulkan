package producer

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/common/logging"
	"github.com/agentstax/vulkan/pkg/producer/batcher"
	"github.com/agentstax/vulkan/pkg/producer/controller"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/google/uuid"
)

// ProducerInstance is a registered producer: it appends messages to the topic
// Register resolved. Shutdown is per call -- a cancelled ctx refuses that
// call's message, the instance itself never stops accepting work.
type ProducerInstance[Message any] struct {
	Topic  *topic.Topic
	Config *ProducerConfig

	controller *controller.ProducerController[Message]
	batcher    *batcher.Batcher[Message]
}

// cfg is already resolved (WithDefaults + Validate) by NewProducer.
func NewProducerInstance[Message any](resolvedTopic *topic.Topic, producerController *controller.ProducerController[Message], cfg *ProducerConfig) (*ProducerInstance[Message], error) {
	if resolvedTopic == nil {
		return nil, errors.New("topic must not be nil")
	}
	if producerController == nil {
		return nil, errors.New("controller must not be nil")
	}
	if cfg == nil {
		return nil, errors.New("config must not be nil")
	}

	topicBatcher, err := batcher.NewBatcher(producerController, resolvedTopic.Id, resolvedTopic.PartitionSize, &cfg.Batch)
	if err != nil {
		return nil, err
	}

	return &ProducerInstance[Message]{
		Topic:      resolvedTopic,
		Config:     cfg,
		controller: producerController,
		batcher:    topicBatcher,
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
func (p *ProducerInstance[Message]) Produce(ctx context.Context, message *Message, options ProduceOptions) (*ProduceResult[Message], error) {
	defer p.warnSlowProduce(ctx, time.Now())
	ctx = logging.WithLogBuffer(ctx)

	options.Message = options.Message.Fill(p.Config.Message)
	if err := options.Validate(); err != nil {
		return nil, err
	}

	// caller keys can collide -- a collision inside a shared txn stalls the
	// whole batch, so keyed calls take a per-call transaction
	if options.IdempotencyKey != uuid.Nil {
		passthrough := func(context.Context, Tx, uuid.UUID) (*Message, error) { return message, nil }
		appended, err := p.controller.AppendMessage(ctx, p.Topic.Id, p.Topic.PartitionSize, passthrough, options)
		if err != nil {
			return nil, err
		}
		return NewProduceResult(appended.Message, appended.Id, appended.Duplicate)
	}

	appended, err := p.batcher.Produce(ctx, message, options)
	if err != nil {
		return nil, err
	}
	return NewProduceResult(appended.Message, appended.Id, appended.Duplicate)
}

// ProduceBatch appends every item in ONE transaction, returning once all are
// durably committed -- none land unless all do. Results are in argument
// order. A batch never shares its transaction with concurrent Produce calls
// or other batches.
//
// Items must not set an IdempotencyKey (see NewProduceItem), so nothing
// dedups across calls: retrying past a ctx cancelled mid-commit (or your
// own crash) can publish every item twice, exactly as with unkeyed Produce.
func (p *ProducerInstance[Message]) ProduceBatch(ctx context.Context, items ...*ProduceItem[Message]) ([]*ProduceResult[Message], error) {
	if len(items) == 0 {
		return nil, errors.New("items must not be empty")
	}
	defer p.warnSlowProduce(ctx, time.Now())
	ctx = logging.WithLogBuffer(ctx)

	appends := make([]*controller.Append[Message], 0, len(items))
	for i, item := range items {
		appendItem, err := p.toAppend(item)
		if err != nil {
			return nil, fmt.Errorf("item %d: %w", i, err)
		}
		appends = append(appends, appendItem)
	}

	appendedRows, failedIdx, err := p.controller.AppendMessageBatch(ctx, p.Topic.Id, p.Topic.PartitionSize, p.Config.Batch.AttemptTimeout, appends)
	if err != nil {
		if failedIdx >= 0 {
			return nil, fmt.Errorf("item %d: %w", failedIdx, err)
		}
		return nil, err
	}

	results := make([]*ProduceResult[Message], 0, len(appendedRows))
	for _, appendedRow := range appendedRows {
		result, err := NewProduceResult(appendedRow.Message, appendedRow.Id, appendedRow.Duplicate)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}

// ProduceFunc appends the message returned by producerFunc, which runs inside
// the message's transaction -- your writes commit or roll back with it.
func (p *ProducerInstance[Message]) ProduceFunc(ctx context.Context, producerFunc ProducerFunc[Message], options ProduceOptions) (*ProduceResult[Message], error) {
	defer p.warnSlowProduce(ctx, time.Now())

	options.Message = options.Message.Fill(p.Config.Message)
	if err := options.Validate(); err != nil {
		return nil, err
	}

	appended, err := p.controller.AppendMessage(ctx, p.Topic.Id, p.Topic.PartitionSize, producerFunc, options)
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
//
// Producing several compaction keys in one transaction locks each key's
// compaction_head row until tx resolves. Two transactions taking the same
// keys in reverse order deadlock: Postgres kills one (40P01, a full
// rollback) and InTransaction never reruns your closure -- retry it
// yourself. Ordering these calls by compaction key avoids the cycle;
// batched Produce sorts the same way, so a consistent order composes.
func (p *ProducerInstance[Message]) ProduceInTx(ctx context.Context, tx Tx, producerFunc ProducerFunc[Message], options ProduceOptions) (*ProduceResult[Message], error) {
	defer p.warnSlowProduce(ctx, time.Now())

	options.Message = options.Message.Fill(p.Config.Message)
	if err := options.Validate(); err != nil {
		return nil, err
	}

	appended, err := p.controller.AppendMessageInTx(ctx, tx, p.Topic.Id, p.Topic.PartitionSize, producerFunc, options)
	if err != nil {
		return nil, err
	}
	return NewProduceResult(appended.Message, appended.Id, appended.Duplicate)
}

// GetCompactionHeadInTx returns the current compaction head under compactionKey,
// or nil if nothing has been published under it.
// It does so within the transaction and locks the found row in a FOR UPDATE
// allowing for race-free compare and set.
func (p *ProducerInstance[Message]) GetCompactionHeadInTx(ctx context.Context, tx Tx, compactionKey string) (*MessageRow[Message], error) {
	return p.controller.GetCompactionHeadInTx(ctx, tx, p.Topic.Id, compactionKey)
}

// warnSlowProduce logs one line when a produce entry point ran past the
// configured threshold -- slowness is its own fact, logged whatever the
// call's outcome.
func (p *ProducerInstance[Message]) warnSlowProduce(ctx context.Context, start time.Time) {
	duration := time.Since(start)
	if p.Config.SlowProduceThreshold <= 0 || duration <= p.Config.SlowProduceThreshold {
		return
	}
	p.Config.Logger.WarnContext(ctx, eventSlowProduce.Message, "code", eventSlowProduce.Code, "topic", p.Topic.Name, "duration", duration, "threshold", p.Config.SlowProduceThreshold)
}

// toAppend shapes one batch item for the controller: fills message options
// and generates the key the datastore's ambiguous-commit rerun dedups on.
func (p *ProducerInstance[Message]) toAppend(item *ProduceItem[Message]) (*controller.Append[Message], error) {
	if item == nil {
		return nil, errors.New("item must not be nil")
	}
	if item.Options.IdempotencyKey != uuid.Nil {
		return nil, errors.New("IdempotencyKey is not supported in a batch -- produce keyed messages individually")
	}

	options := item.Options
	options.Message = options.Message.Fill(p.Config.Message)
	idempotencyKey, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	options.IdempotencyKey = idempotencyKey
	return controller.NewAppend(item.Message, options)
}
