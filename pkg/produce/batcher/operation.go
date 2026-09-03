package batcher

import (
	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/produce"
	"github.com/agentstax/vulkan/pkg/produce/controller"
)

// batchOperation is one produce in flight: what to write, and how the
// outcome gets back to the waiting caller.
type batchOperation[Message common.Versioned] struct {
	request  *batchRequest[Message]
	response *batchResponse[Message]
}

func newBatchOperation[Message common.Versioned](message *Message, options produce.ProduceOptions) *batchOperation[Message] {
	return &batchOperation[Message]{
		request:  newBatchRequest(message, options),
		response: newBatchResponse[Message](),
	}
}

// batchRequest is the pure input of one produce: message, options.
type batchRequest[Message common.Versioned] struct {
	message *Message
	options produce.ProduceOptions // Options.IdempotencyKey is minted at enqueue, reused across every rerun of the batch
}

func newBatchRequest[Message common.Versioned](message *Message, options produce.ProduceOptions) *batchRequest[Message] {
	return &batchRequest[Message]{
		message: message,
		options: options,
	}
}

type batchResponse[Message common.Versioned] struct {
	done chan struct{} // closed by record
	err  error         // written before close(done), read only after <-done

	// appended is written by the batch worker before close(done) and read
	// only after <-done -- the last attempt's value wins
	appended controller.Appended[Message]
}

func newBatchResponse[Message common.Versioned]() *batchResponse[Message] {
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
