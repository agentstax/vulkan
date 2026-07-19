package producer

import (
	"cmp"
	"slices"
)

// batch is the operations dequeued together and resolved in one transaction.
type batch[Message any] struct {
	operations []*batchOperation[Message]
}

func newBatch[Message any](operations []*batchOperation[Message]) *batch[Message] {
	// ascending CompactionKey -> every batch txn takes its latest_key row
	// locks in one global order: hot keys queue batch-to-batch, never deadlock
	slices.SortStableFunc(operations, func(a, b *batchOperation[Message]) int {
		return cmp.Compare(a.request.opts.CompactionKey, b.request.opts.CompactionKey)
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
	for _, op := range b.operations {
		op.response.record(err)
	}
}
