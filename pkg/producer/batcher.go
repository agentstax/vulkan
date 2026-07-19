package producer

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// batcher groups concurrent payload-only produces for one topic into shared
// transactions, amortizing the per-commit fsync in the database.
type batcher[Message any] struct {
	datastore     *producerDatastore[Message]
	topicID       int64
	partitionSize int64
	cfg           ProducerDatastoreConfig

	queue workQueue[batchOperation[Message]]
}

func newBatcher[Message any](datastore *producerDatastore[Message], topicID, partitionSize int64, cfg ProducerDatastoreConfig) *batcher[Message] {
	return &batcher[Message]{
		datastore:     datastore,
		topicID:       topicID,
		partitionSize: partitionSize,
		cfg:           cfg,
	}
}

// produce enqueues one message and blocks until its batch commits (durable) or fails.
func (b *batcher[Message]) produce(ctx context.Context, message *Message, opts ProduceOptions) (*Message, error) {
	// already cancelled -> fail BEFORE enqueue. This is the graceful shutdown path:
	// a cancelled producer refuses new work while enqueued work resolves.
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("produce rejected before enqueue for topic %d, nothing was published: %w", b.topicID, err)
	}

	// always minted fresh -- fresh V7 keys cannot collide inside the shared txn
	idempotencyKey, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("minting idempotency key for topic %d: %w", b.topicID, err)
	}

	operation := newBatchOperation(idempotencyKey, message, opts)

	b.queue.enqueue(operation)
	if b.queue.needsWorker(b.cfg.BatchMaxSize, b.cfg.BatchConcurrencyLimit) {
		go b.work()
	}

	select {
	case <-operation.response.Done():
		// continue past select
	case <-ctx.Done():
		// exit early with no shutdownGrace
		if b.cfg.BatchShutdownGrace < 0 {
			return nil, fmt.Errorf("produce abandoned for topic %d, batch outcome ambiguous (BatchShutdownGrace < 0): %w", b.topicID, ctx.Err())
		}

		// enqueued work cannot be recalled -- wait up to the grace for the
		// real outcome before abandoning as ambiguous
		grace := time.NewTimer(b.cfg.BatchShutdownGrace)
		defer grace.Stop()

		select {
		case <-operation.response.Done():
			// ideally this completes -> graceful shutdown
			b.datastore.Logger.DebugContext(ctx, "cancelled produce resolved within shutdown grace", "topic_id", b.topicID)
		case <-grace.C:
			// if shutdownGrace times out -> exit early
			// work commit status is ambiguous and should be retried if possible when supplying external idempotency key
			b.datastore.Logger.WarnContext(ctx, "produce abandoned after shutdown grace, batch outcome ambiguous", "topic_id", b.topicID, "grace", b.cfg.BatchShutdownGrace)
			return nil, fmt.Errorf("produce abandoned after BatchShutdownGrace (%s) for topic %d, batch outcome ambiguous: %w", b.cfg.BatchShutdownGrace, b.topicID, ctx.Err())
		}
	}

	if err := operation.response.Err(); err != nil {
		return nil, err
	}
	return message, nil
}

// work fires batches one after the other until the queue empties.
func (b *batcher[Message]) work() {
	// the worker's own context, never a caller's: a caller cancelling stops
	// waiting, it must not abort a transaction other operations share
	ctx := context.Background()
	for {
		operations := b.queue.dequeue(b.cfg.BatchMaxSize)
		if operations == nil {
			return
		}
		b.resolveBatch(ctx, newBatch(operations))
	}
}
