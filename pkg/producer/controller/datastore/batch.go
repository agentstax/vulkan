package datastore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// AppendMessageBatch commits every append in ONE transaction, absorbing the
// two recoverable failures: transient errors (retried, each attempt bounded
// by attemptTimeout) and a missing partition (healed). failedIdx is the FIRST
// failure in pipeline order, -1 when the failure carries no index.
func (d *ProducerDatastore[Message]) AppendMessageBatch(ctx context.Context, topicId int64, partitionSize int64, attemptTimeout time.Duration, appends []*AppendData[Message]) ([]AppendedData[Message], int, error) {
	appended, failedIdx, err := d.appendMessageBatch(ctx, topicId, attemptTimeout, appends)
	if isMissingPartition(err) {
		d.Logger.WarnContext(ctx, "no partition covers the next message id -- creating it", "topic_id", topicId)
		healErr := d.DatastoreRetry.Wrap(ctx, func() error {
			return d.ensureCoveringPartition(ctx, topicId, partitionSize)
		})
		if healErr != nil {
			return nil, -1, healErr
		}
		// a partition miss persisting past the heal is terminal
		appended, failedIdx, err = d.appendMessageBatch(ctx, topicId, attemptTimeout, appends)
	}
	if err != nil {
		return appended, failedIdx, err
	}

	firstId, lastId := appendedIdRange(appended)
	if d.createAheadGate.shouldTriggerWithRange(topicId, partitionSize, firstId, lastId) {
		d.createPartitionAhead(topicId, partitionSize)
	}
	return appended, failedIdx, nil
}

// appendMessageBatch reruns one-attempt transactions under the transient-retry
// policy; the last attempt wins failedIdx.
func (d *ProducerDatastore[Message]) appendMessageBatch(ctx context.Context, topicId int64, attemptTimeout time.Duration, appends []*AppendData[Message]) (appended []AppendedData[Message], failedIdx int, err error) {
	failedIdx = -1
	err = d.DatastoreRetry.Wrap(ctx, func() error {
		// bound each attempt -- a hung database must not hold the batch forever
		attemptCtx, cancel := context.WithTimeoutCause(ctx, attemptTimeout,
			fmt.Errorf("batch attempt exceeded BatchAttemptTimeout (%s) for topic %d", attemptTimeout, topicId))
		defer cancel()

		results, idx, err := d.appendMessageBatchTransaction(attemptCtx, topicId, appends)
		if err != nil && attemptCtx.Err() != nil {
			// the wire error alone doesn't say WHOSE deadline expired
			err = fmt.Errorf("%w: %w", err, context.Cause(attemptCtx))
		}
		appended, failedIdx = results, idx
		return err
	})
	return appended, failedIdx, err
}

// appendMessageBatchTransaction is one attempt: ONE plain transaction, every
// query batched into a single round trip, no savepoints.
func (d *ProducerDatastore[Message]) appendMessageBatchTransaction(ctx context.Context, topicId int64, appends []*AppendData[Message]) ([]AppendedData[Message], int, error) {
	tx, err := d.Datastore.Pool.Begin(ctx)
	if err != nil {
		return nil, -1, err
	}

	// If Commit() is called successfully, Rollback() becomes a no-op and returns pgx.ErrTxClosed.
	defer tx.Rollback(ctx)

	statements := &pgx.Batch{}
	for _, data := range appends {
		sql, args := protectedInsertSQL(topicId, data.Payload, data)
		statements.Queue(sql, args...)
	}

	appended := make([]AppendedData[Message], len(appends))
	br := tx.SendBatch(ctx, statements)
	for i, data := range appends {
		var id int64
		err := br.QueryRow().Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			// claim already existed -- this message is already durable from an
			// earlier ambiguous commit of the same batch. Zero-row no-op.
			d.Logger.DebugContext(ctx, "duplicate publish detected, idempotency claim already existed", "topic_id", topicId, "idempotency_key", data.IdempotencyKey)
			appended[i] = AppendedData[Message]{Message: data.Payload, Duplicate: true}
			continue
		}
		if err != nil {
			br.Close()
			// results past the first failure carry no information
			return nil, i, err
		}
		appended[i] = AppendedData[Message]{Message: data.Payload, Id: id}
	}
	if err := br.Close(); err != nil {
		return nil, -1, err
	}

	// an error here is ambiguous -- the commit may have landed. A rerun under
	// the same keys turns anything that did into the duplicate no-op above.
	if err := tx.Commit(ctx); err != nil {
		return nil, -1, err
	}
	return appended, -1, nil
}

// appendedIdRange returns the first and last inserted ids, skipping the zero
// ids of duplicates; (0, 0) when nothing new was inserted.
func appendedIdRange[Message any](appended []AppendedData[Message]) (int64, int64) {
	var firstId int64
	var lastId int64
	for _, data := range appended {
		if data.Id == 0 {
			continue
		}
		if firstId == 0 {
			firstId = data.Id
		}
		lastId = data.Id
	}
	return firstId, lastId
}
