package batcher

import (
	"cmp"
	"slices"

	"github.com/agentstax/vulkan/pkg/common"
)

// batch is the operations dequeued together and resolved in one transaction.
type batch[Message common.Versioned] struct {
	operations []*batchOperation[Message]
}

func newBatch[Message common.Versioned](operations []*batchOperation[Message]) *batch[Message] {
	// ascending message key (compacted only) -> every batch txn takes its
	// compaction_head row locks in one global order: hot keys queue
	// batch-to-batch, never deadlock
	slices.SortStableFunc(operations, func(a, b *batchOperation[Message]) int {
		return cmp.Compare(compactionSortKey(a), compactionSortKey(b))
	})
	return &batch[Message]{operations: operations}
}

func (b *batch[Message]) size() int {
	return len(b.operations)
}

func (b *batch[Message]) all() []*batchOperation[Message] {
	return b.operations
}

func (b *batch[Message]) at(i int) *batchOperation[Message] {
	return b.operations[i]
}

// remove returns the batch without the operation at index i.
func (b *batch[Message]) remove(i int) *batch[Message] {
	// Concat, NOT slices.Delete -- returns a new batch, never mutates the receiver
	return newBatch(slices.Concat(b.operations[:i], b.operations[i+1:]))
}

// single returns a new one-operation batch holding index i.
func (b *batch[Message]) single(i int) *batch[Message] {
	return newBatch([]*batchOperation[Message]{b.operations[i]})
}

func (b *batch[Message]) recordAll(err error) {
	for _, operation := range b.operations {
		operation.response.record(err)
	}
}

// ***************
// *** HELPERS ***
// ***************

// compactionSortKey reads an operation's message key when the produce enabled
// compaction; "" for a message that takes no compaction_head lock.
func compactionSortKey[Message common.Versioned](operation *batchOperation[Message]) string {
	options := operation.request.options
	if options.Compaction == nil || !options.Compaction.Enable {
		return ""
	}
	return options.MessageKey
}
