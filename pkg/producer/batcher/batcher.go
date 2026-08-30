package batcher

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/producer/controller"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/google/uuid"
)

// Batcher groups concurrent payload-only produces for one topic into shared
// transactions, amortizing the per-commit fsync in the database.
type Batcher[Message topic.Versioned] struct {
	Config *BatcherConfig

	controller    *controller.ProducerController
	topicId       int64
	partitionSize int64

	queue workQueue[batchOperation[Message]]
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewBatcher[Message topic.Versioned](producerController *controller.ProducerController, topicId int64, partitionSize int64, cfg *BatcherConfig) (*Batcher[Message], error) {
	if producerController == nil {
		return nil, errors.New("controller must not be nil")
	}
	if topicId <= 0 {
		return nil, fmt.Errorf("topicId must be > 0, got %d", topicId)
	}
	if cfg == nil {
		cfg = &BatcherConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &Batcher[Message]{
		Config:        cfg,
		controller:    producerController,
		topicId:       topicId,
		partitionSize: partitionSize,
	}, nil
}

// Produce enqueues one message and blocks until its batch commits (durable) or fails.
func (b *Batcher[Message]) Produce(ctx context.Context, message *Message, options controller.ProduceOptions) (*controller.Appended[Message], error) {
	// already cancelled -> fail BEFORE enqueue. This is the graceful shutdown path:
	// a cancelled producer refuses new work while enqueued work resolves.
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("produce rejected before enqueue for topic %d, nothing was published: %w", b.topicId, err)
	}

	// always minted fresh -- fresh V7 keys cannot collide inside the shared txn
	idempotencyKey, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("minting idempotency key for topic %d: %w", b.topicId, err)
	}
	options.IdempotencyKey = idempotencyKey.String()

	operation := newBatchOperation(message, options)

	b.queue.enqueue(operation)
	if b.queue.needsWorker(b.Config.MaxSize, b.Config.ConcurrencyLimit) {
		go b.work()
	}

	select {
	case <-operation.response.done:
		// continue past select
	case <-ctx.Done():
		// exit early with no shutdownGrace
		if b.Config.ShutdownGrace < 0 {
			return nil, fmt.Errorf("produce abandoned for topic %d, batch outcome ambiguous (ShutdownGrace < 0): %w", b.topicId, ctx.Err())
		}

		// enqueued work cannot be recalled -- wait up to the grace for the
		// real outcome before abandoning as ambiguous
		grace := time.NewTimer(b.Config.ShutdownGrace)
		defer grace.Stop()

		select {
		case <-operation.response.done:
			// ideally this completes -> graceful shutdown
			b.Config.Logger.DebugContext(ctx, "cancelled produce resolved within shutdown grace", "topic_id", b.topicId)
		case <-grace.C:
			// if shutdownGrace times out -> exit early
			// work commit status is ambiguous and should be retried if possible when supplying external idempotency key
			return nil, fmt.Errorf("produce abandoned after ShutdownGrace (%s) for topic %d, batch outcome ambiguous: %w", b.Config.ShutdownGrace, b.topicId, ctx.Err())
		}
	}

	if err := operation.response.err; err != nil {
		return nil, err
	}
	appended := operation.response.appended
	return &appended, nil
}

// work fires batches one after the other until the queue empties.
func (b *Batcher[Message]) work() {
	// the worker's own context, never a caller's: a caller cancelling stops
	// waiting, it must not abort a transaction other operations share
	ctx := context.Background()
	for {
		operations := b.queue.dequeue(b.Config.MaxSize)
		if operations == nil {
			return
		}
		b.resolveBatch(ctx, newBatch(operations))
	}
}
