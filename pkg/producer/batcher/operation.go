package batcher

import (
	"github.com/agentstax/vulkan/pkg/producer/controller"
)

// batchOperation is one produce in flight: what to write, and how the
// outcome gets back to the waiting caller.
type batchOperation[Message any] struct {
	request  *batchRequest[Message]
	response *batchResponse[Message]
}

func newBatchOperation[Message any](message *Message, options controller.ProduceOptions) *batchOperation[Message] {
	return &batchOperation[Message]{
		request:  newBatchRequest(message, options),
		response: newBatchResponse[Message](),
	}
}

// batchRequest is the pure input of one produce: message, options.
type batchRequest[Message any] struct {
	message *Message
	options controller.ProduceOptions // Options.IdempotencyKey is minted at enqueue, reused across every rerun of the batch
}

func newBatchRequest[Message any](message *Message, options controller.ProduceOptions) *batchRequest[Message] {
	return &batchRequest[Message]{
		message: message,
		options: options,
	}
}

type batchResponse[Message any] struct {
	done chan struct{} // closed by record
	err  error         // written before close(done), read only after <-done

	// appended is written by the batch worker before close(done) and read
	// only after <-done -- the last attempt's value wins
	appended controller.Appended[Message]
}

func newBatchResponse[Message any]() *batchResponse[Message] {
	return &batchResponse[Message]{
		done: make(chan struct{}),
	}
}

func (r *batchResponse[Message]) recordAppended(appended controller.Appended[Message]) {
	r.appended = appended
}

func (r *batchResponse[Message]) record(err error) {
	r.err = err
	close(r.done) // a second record panics here -- every operation gets exactly one outcome
}
