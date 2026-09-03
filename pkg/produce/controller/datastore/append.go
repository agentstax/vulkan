package datastore

import (
	"context"

	"github.com/agentstax/vulkan/pkg/common"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/produce"
)

// AppendMessage commits one message in its own transaction, self-healing a
// missing partition and retrying transient errors. The caller resolves
// data.IdempotencyKey once, outside the retry -- that's what makes a retried
// attempt safe after an ambiguous commit instead of a double-publish.
func (d *ProduceDatastore) AppendMessage[Message common.Versioned](ctx context.Context, topicId int64, partitionSize int64, produceFunc produce.ProducerFunc[Message], data *Append[Message]) (*Appended[Message], error) {
	var appended *Appended[Message]
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		appended, err = d.appendMessage(ctx, topicId, partitionSize, produceFunc, data)
		return err
	})
	return appended, err
}

// appendMessage runs the append's transaction until a partition covers it.
// Rerunning produceFunc is safe because its writes all go through the tx
// that just rolled back.
func (d *ProduceDatastore) appendMessage[Message common.Versioned](ctx context.Context, topicId int64, partitionSize int64, produceFunc produce.ProducerFunc[Message], data *Append[Message]) (*Appended[Message], error) {
	var appended *Appended[Message]
	err := d.insertUntilCovered(ctx, topicId, partitionSize, func() error {
		var err error
		appended, err = d.appendMessageTransaction(ctx, topicId, produceFunc, data)
		return err
	})
	if err != nil {
		return nil, err
	}

	if d.createAheadGate.shouldTriggerWithId(topicId, partitionSize, appended.Id) {
		d.createPartitionAhead(topicId, partitionSize, appended.Id)
	}
	return appended, nil
}

// appendMessageTransaction opens the append's own transaction: produceFunc +
// the claim-protected insert, committed together.
func (d *ProduceDatastore) appendMessageTransaction[Message common.Versioned](ctx context.Context, topicId int64, produceFunc produce.ProducerFunc[Message], data *Append[Message]) (*Appended[Message], error) {
	var appended *Appended[Message]

	// the one genuinely ambiguous point -- a blip AT Commit loses the commit
	// confirmation, not whether it landed. idempotency_key's ON CONFLICT DO NOTHING
	// makes a retry safe.
	err := iDatastore.InTransaction(ctx, d.Datastore, func(ctx context.Context, tx iDatastore.Tx) error {
		var err error
		appended, err = d.runInsert(ctx, tx, topicId, produceFunc, data)
		return err
	})
	if err != nil {
		return nil, err
	}
	return appended, nil
}

// AppendMessageInTx runs produceFunc + the message insert against a
// caller-supplied tx -- no Begin/Commit/Rollback, that's owned by whoever
// opened tx. Self-heals a missing partition inside its own SAVEPOINT
// (runInsertSavepoint), so the rerun can't undo an earlier target's insert
// or rerun a caller side effect between calls. No transient retry: the tx
// owns its own error handling.
func (d *ProduceDatastore) AppendMessageInTx[Message common.Versioned](ctx context.Context, tx iDatastore.Tx, topicId int64, partitionSize int64, produceFunc produce.ProducerFunc[Message], data *Append[Message]) (*Appended[Message], error) {
	var appended *Appended[Message]
	err := d.insertUntilCovered(ctx, topicId, partitionSize, func() error {
		var err error
		appended, err = d.runInsertSavepoint(ctx, tx, topicId, produceFunc, data)
		return err
	})
	if err != nil {
		return nil, err
	}

	// pre-commit on purpose: a rollback burns the id either way, and an early
	// empty partition is harmless. The CREATE waits on this tx's own parent
	// lock, so its first attempts back off until the caller commits.
	if d.createAheadGate.shouldTriggerWithId(topicId, partitionSize, appended.Id) {
		d.createPartitionAhead(topicId, partitionSize, appended.Id)
	}
	return appended, nil
}
