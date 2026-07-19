package producer

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// attemptBatch runs the batch to a terminal outcome, absorbing the two
// recoverable failures: transient errors (retried) and a missing partition
// (healed).
func (b *batcher[Message]) attemptBatch(ctx context.Context, batch *batch[Message]) (failedIdx int, err error) {
	failedIdx, err = b.runBatchWithRetry(ctx, batch)
	if err == nil || !isMissingPartition(err) {
		return failedIdx, err
	}

	if healErr := b.healMissingPartition(ctx); healErr != nil {
		return -1, healErr
	}
	// a partition miss persisting past the heal is terminal
	return b.runBatchWithRetry(ctx, batch)
}

// runBatchWithRetry reruns runBatch under the transient-retry policy.
func (b *batcher[Message]) runBatchWithRetry(ctx context.Context, batch *batch[Message]) (failedIdx int, err error) {
	failedIdx = -1
	err = b.datastore.Retry.Wrap(ctx, func() error {
		// bound each attempt -- a hung database must not hold the batch forever
		attemptCtx, cancel := context.WithTimeoutCause(ctx, b.cfg.BatchAttemptTimeout,
			fmt.Errorf("batch attempt exceeded BatchAttemptTimeout (%s) for topic %d", b.cfg.BatchAttemptTimeout, b.topicID))
		defer cancel()

		idx, err := b.runBatch(attemptCtx, batch)
		if err != nil && attemptCtx.Err() != nil {
			// the wire error alone doesn't say WHOSE deadline expired
			err = fmt.Errorf("%w: %w", err, context.Cause(attemptCtx))
		}
		// the last attempt wins failedIdx -- a permanent error stops Wrap
		// right here, having either set an index or kept it -1
		failedIdx = idx
		return err
	})

	return failedIdx, err
}

// runBatch is one attempt: ONE plain transaction, every query batched into a
// single round trip, no savepoints. failedIdx is the FIRST failure in
// pipeline order.
func (b *batcher[Message]) runBatch(ctx context.Context, batch *batch[Message]) (failedIdx int, err error) {
	tx, err := b.datastore.Datastore.Pool.Begin(ctx)
	if err != nil {
		return -1, err
	}

	// If Commit() is called successfully, Rollback() becomes a no-op and returns pgx.ErrTxClosed.
	defer tx.Rollback(ctx)

	statements := &pgx.Batch{}
	for _, op := range batch.all() {
		r := op.request
		sql, args := protectedInsertSQL(b.topicID, r.idempotencyKey, r.message, r.opts)
		statements.Queue(sql, args...)
	}

	br := tx.SendBatch(ctx, statements)
	for i, op := range batch.all() {
		var id int64
		err := br.QueryRow().Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			// claim already existed -- this message is already durable from an
			// earlier ambiguous commit of the same batch. Zero-row no-op.
			b.datastore.Logger.DebugContext(ctx, "duplicate publish detected, idempotency claim already existed", "topic_id", b.topicID, "idempotency_key", op.request.idempotencyKey)
			continue
		}
		if err != nil {
			br.Close()
			// results past the first failure carry no information
			return i, err
		}
	}
	if err := br.Close(); err != nil {
		return -1, err
	}

	// an error here is ambiguous -- the commit may have landed. A rerun under
	// the same keys turns anything that did into the duplicate no-op above.
	if err := tx.Commit(ctx); err != nil {
		return -1, err
	}
	return -1, nil
}

// healMissingPartition creates the one partition past head -- a redundancy
// backstop for a burst of messages that outran the janitor's create-ahead.
func (b *batcher[Message]) healMissingPartition(ctx context.Context) error {
	b.datastore.Logger.WarnContext(ctx, "publish outran janitor create-ahead, self-healing missing partition", "topic_id", b.topicID)
	return b.datastore.Retry.Wrap(ctx, func() error {
		return b.datastore.ensureCoveringPartition(ctx, b.topicID, b.partitionSize)
	})
}
