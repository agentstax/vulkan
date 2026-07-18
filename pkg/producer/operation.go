package producer

import "github.com/google/uuid"

// batchOperation is one produce in flight: what to write, and how the
// outcome gets back to the waiting caller.
type batchOperation[Message any] struct {
	request  *batchRequest[Message]
	response *batchResponse
}

func newBatchOperation[Message any](idempotencyKey uuid.UUID, message *Message, opts ProduceOptions) *batchOperation[Message] {
	return &batchOperation[Message]{
		request:  newBatchRequest(idempotencyKey, message, opts),
		response: newBatchResponse(),
	}
}

// batchRequest is the pure input of one produce: key, payload, options.
type batchRequest[Message any] struct {
	// minted once at enqueue, reused across every rerun of the batch
	idempotencyKey uuid.UUID
	message        *Message
	opts           ProduceOptions
}

func newBatchRequest[Message any](idempotencyKey uuid.UUID, message *Message, opts ProduceOptions) *batchRequest[Message] {
	return &batchRequest[Message]{
		idempotencyKey: idempotencyKey,
		message:        message,
		opts:           opts,
	}
}

type batchResponse struct {
	done chan struct{} // closed by record
	err  error         // written before close(done), read only after <-done
}

func newBatchResponse() *batchResponse {
	return &batchResponse{
		done: make(chan struct{}),
	}
}

func (r *batchResponse) record(err error) {
	r.err = err
	close(r.done) // a second record panics here -- every operation gets exactly one outcome
}

func (r *batchResponse) Done() <-chan struct{} {
	return r.done
}

// Err is only valid after Done is closed.
func (r *batchResponse) Err() error {
	return r.err
}
