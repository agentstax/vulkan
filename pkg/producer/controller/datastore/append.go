package datastore

import (
	"context"
)

// AppendMessage commits one message in its own transaction, self-healing a
// missing partition and retrying transient errors. The caller resolves
// data.IdempotencyKey once, outside the retry -- that's what makes a retried
// attempt safe after an ambiguous commit instead of a double-publish.
func (d *ProducerDatastore[Message]) AppendMessage(ctx context.Context, topicId int64, partitionSize int64, produceFunc ProduceFunc[Message], data *AppendData[Message]) (*AppendedData[Message], error) {
	var appended *AppendedData[Message]
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		appended, err = d.appendMessage(ctx, topicId, partitionSize, produceFunc, data)
		return err
	})
	return appended, err
}

// appendMessage self-heals a missing-partition insert: the first insert past
// a partition boundary fails -> creates the partition -> and retries.
func (d *ProducerDatastore[Message]) appendMessage(ctx context.Context, topicId int64, partitionSize int64, produceFunc ProduceFunc[Message], data *AppendData[Message]) (*AppendedData[Message], error) {
	appended, err := d.appendMessageTransaction(ctx, topicId, produceFunc, data)
	if isMissingPartition(err) {
		d.Logger.WarnContext(ctx, "no partition covers the next message id -- creating it", "topic_id", topicId)
		if healErr := d.ensureCoveringPartition(ctx, topicId, partitionSize); healErr != nil {
			return nil, healErr
		}

		// Rerunning produceFunc is safe because its
		// writes all go through the tx that just rolled back
		appended, err = d.appendMessageTransaction(ctx, topicId, produceFunc, data)
	}
	if err != nil {
		return nil, err
	}

	if d.createAheadGate.shouldTriggerWithId(topicId, partitionSize, appended.Id) {
		d.createPartitionAhead(topicId, partitionSize)
	}
	return appended, nil
}

// appendMessageTransaction opens the append's own transaction: produceFunc +
// the claim-protected insert, committed together.
func (d *ProducerDatastore[Message]) appendMessageTransaction(ctx context.Context, topicId int64, produceFunc ProduceFunc[Message], data *AppendData[Message]) (*AppendedData[Message], error) {
	tx, err := d.Datastore.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}

	// If Commit() is called successfully, Rollback() becomes a no-op and returns pgx.ErrTxClosed.
	defer tx.Rollback(ctx)

	appended, err := d.runInsert(ctx, newTx(tx), topicId, produceFunc, data)
	if err != nil {
		return nil, err
	}

	if appended.Duplicate {
		// claim already existed -- a retried call under the same key that's
		// already durable. Nothing new to commit, but the transaction we
		// opened above still needs closing.
		if err := tx.Commit(ctx); err != nil {
			return nil, err // nothing new was written -- safe for Retry to auto-classify
		}
		return appended, nil
	}

	// the one genuinely ambiguous point -- a blip AT Commit loses the commit
	// confirmation, not whether it landed. idempotency_key's ON CONFLICT DO NOTHING
	// makes a retry safe.
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}

	return appended, nil
}

// AppendMessageInTx runs produceFunc + the message insert against a
// caller-supplied tx -- no Begin/Commit/Rollback, that's owned by whoever
// opened tx. Self-heals a missing partition inside its own SAVEPOINT
// (runInsertSavepoint), so retrying here can't undo an earlier target's
// insert or rerun a caller side effect between calls. No retry: the tx owns
// its own error handling.
func (d *ProducerDatastore[Message]) AppendMessageInTx(ctx context.Context, tx Tx, topicId int64, partitionSize int64, produceFunc ProduceFunc[Message], data *AppendData[Message]) (*AppendedData[Message], error) {
	appended, err := d.runInsertSavepoint(ctx, tx, topicId, produceFunc, data)
	if isMissingPartition(err) {
		d.Logger.WarnContext(ctx, "no partition covers the next message id -- creating it", "topic_id", topicId)
		if healErr := d.ensureCoveringPartition(ctx, topicId, partitionSize); healErr != nil {
			return nil, healErr
		}
		appended, err = d.runInsertSavepoint(ctx, tx, topicId, produceFunc, data)
	}
	if err != nil {
		return nil, err
	}

	// pre-commit on purpose: a rollback burns the id either way, and an early
	// empty partition is harmless. The CREATE waits on this tx's own parent
	// lock, so its first attempts back off until the caller commits.
	if d.createAheadGate.shouldTriggerWithId(topicId, partitionSize, appended.Id) {
		d.createPartitionAhead(topicId, partitionSize)
	}
	return appended, nil
}
