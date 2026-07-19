package producer

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// bounds how many messages share one transaction -- caps lock-hold, latency
// tail, and the rerun cost of evicting poison
const defaultMaxBatchSize = 100

// bounds how many workers drain the queue at once
const defaultConcurrencyLimit = 4

// bounds one batch transaction attempt against the database
const defaultAttemptTimeout = 10 * time.Second

// bounds how long a cancelled produce waits for its real outcome -- keep
// above the attempt timeout
const defaultShutdownGrace = 15 * time.Second

// batchSettings are the batcher knobs, resolved from ProducerDatastoreConfig.
type batchSettings struct {
	maxBatchSize     int
	concurrencyLimit int
	attemptTimeout   time.Duration
	shutdownGrace    time.Duration // negative = abandon immediately on cancel
}

func (s batchSettings) withDefaults() batchSettings {
	if s.maxBatchSize < 1 {
		s.maxBatchSize = defaultMaxBatchSize
	}
	if s.concurrencyLimit < 1 {
		s.concurrencyLimit = defaultConcurrencyLimit
	}
	if s.attemptTimeout == 0 {
		s.attemptTimeout = defaultAttemptTimeout
	}
	if s.shutdownGrace == 0 {
		s.shutdownGrace = defaultShutdownGrace
	}
	return s
}

// batcher groups concurrent payload-only produces for one topic into shared
// transactions, amortizing the per-commit fsync in the database.
type batcher[Message any] struct {
	datastore     *producerDatastore[Message]
	topicID       int64
	partitionSize int64
	settings      batchSettings

	queue workQueue[batchOperation[Message]]
}

func newBatcher[Message any](datastore *producerDatastore[Message], topicID, partitionSize int64, settings batchSettings) *batcher[Message] {
	return &batcher[Message]{
		datastore:     datastore,
		topicID:       topicID,
		partitionSize: partitionSize,
		settings:      settings.withDefaults(),
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
	if b.queue.needsWorker(b.settings.maxBatchSize, b.settings.concurrencyLimit) {
		go b.work()
	}

	select {
	case <-operation.response.Done():
		// continue past select
	case <-ctx.Done():
		// exit early with no shutdownGrace
		if b.settings.shutdownGrace < 0 {
			return nil, fmt.Errorf("produce abandoned for topic %d, batch outcome ambiguous (BatchShutdownGrace < 0): %w", b.topicID, ctx.Err())
		}

		// enqueued work cannot be recalled -- wait up to the grace for the
		// real outcome before abandoning as ambiguous
		grace := time.NewTimer(b.settings.shutdownGrace)
		defer grace.Stop()

		select {
		case <-operation.response.Done():
			// ideally this completes -> graceful shutdown
			b.datastore.Logger.DebugContext(ctx, "cancelled produce resolved within shutdown grace", "topic_id", b.topicID)
		case <-grace.C:
			// if shutdownGrace times out -> exit early
			// work commit status is ambiguous and should be retried if possible when supplying external idempotency key
			b.datastore.Logger.WarnContext(ctx, "produce abandoned after shutdown grace, batch outcome ambiguous", "topic_id", b.topicID, "grace", b.settings.shutdownGrace)
			return nil, fmt.Errorf("produce abandoned after BatchShutdownGrace (%s) for topic %d, batch outcome ambiguous: %w", b.settings.shutdownGrace, b.topicID, ctx.Err())
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
		operations := b.queue.dequeue(b.settings.maxBatchSize)
		if operations == nil {
			return
		}
		b.resolveBatch(ctx, newBatch(operations))
	}
}
