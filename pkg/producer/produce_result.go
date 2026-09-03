package producer

import (
	"errors"

	"github.com/agentstax/vulkan/pkg/common"
)

// ProduceResult is one produce call's outcome.
type ProduceResult[Message common.Versioned] struct {
	// Message - the payload this call built.
	//
	// On a duplicate it is NOT the originally-stored payload: the idempotency
	// table records only the key, so the original is unrecoverable by design.
	Message *Message `json:"message"`

	// Id - the stored message id; 0 when Duplicate.
	Id int64 `json:"message_id"`

	// Duplicate - the idempotency claim already existed: an earlier call, or
	// an earlier attempt of this one after an ambiguous commit, already
	// published under the same IdempotencyKey.
	Duplicate bool `json:"duplicate"`
}

func NewProduceResult[Message common.Versioned](message *Message, id int64, duplicate bool) (*ProduceResult[Message], error) {
	if message == nil {
		return nil, errors.New("message must not be nil")
	}
	return &ProduceResult[Message]{
		Message:   message,
		Id:        id,
		Duplicate: duplicate,
	}, nil
}
