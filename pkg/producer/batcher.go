package producer

import (
	"context"

	"github.com/google/uuid"
)

// bounds how many messages share one transaction -- caps lock-hold, latency
// tail, and the rerun cost of evicting poison
const defaultMaxBatchSize = 100

// bounds how many workers drain the queue at once
const defaultConcurrencyLimit = 4

// batcher groups concurrent payload-only produces for one topic into shared
// transactions, amortizing the per-commit fsync in the database.
type batcher[Message any] struct {
	datastore        *producerDatastore[Message]
	topicID          int64
	partitionSize    int64
	maxBatchSize     int
	concurrencyLimit int

	queue workQueue[batchOperation[Message]]
}

func newBatcher[Message any](datastore *producerDatastore[Message], topicID, partitionSize int64, maxBatchSize int) *batcher[Message] {
	if maxBatchSize < 1 {
		maxBatchSize = defaultMaxBatchSize
	}
	return &batcher[Message]{
		datastore:        datastore,
		topicID:          topicID,
		partitionSize:    partitionSize,
		maxBatchSize:     maxBatchSize,
		concurrencyLimit: defaultConcurrencyLimit,
	}
}

// produce enqueues one message and blocks until its batch commits (durable) or fails.
func (b *batcher[Message]) produce(ctx context.Context, message *Message, opts ProduceOptions) (*Message, error) {
	// always minted fresh, never caller-supplied: keys colliding inside
	// shared transactions serialize globally (ON CONFLICT waits on the
	// holding txn), and fresh V7 keys cannot collide
	idempotencyKey, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}

	operation := newBatchOperation(idempotencyKey, message, opts)

	b.queue.enqueue(operation)
	if b.queue.needsWorker(b.maxBatchSize, b.concurrencyLimit) {
		go b.work()
	}

	select {
	case <-operation.response.Done():
		if err := operation.response.Err(); err != nil {
			return nil, err
		}
		return message, nil
	case <-ctx.Done():
		// stops the wait, not the message: the worker still commits it, so
		// this outcome is ambiguous -- same as cancelling mid-commit
		return nil, ctx.Err()
	}
}

// work fires batches one after the other until the queue empties.
func (b *batcher[Message]) work() {
	// the worker's own context, never a caller's: a caller cancelling stops
	// waiting, it must not abort a transaction other operations share
	ctx := context.Background()
	for {
		operations := b.queue.dequeue(b.maxBatchSize)
		if operations == nil {
			return
		}
		b.resolveBatch(ctx, newBatch(operations))
	}
}
