package controller

import (
	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/topic"
)

// MessageData is one stored message, typed; the struct and its docs live in
// pkg/common.
type MessageData[Message topic.Versioned] = common.MessageData[Message]

// Appended is one append's outcome.
type Appended[Message topic.Versioned] struct {
	// Message - the payload this call built. On a duplicate it is NOT the
	// originally-stored payload: the idempotency table records only the key.
	Message *Message

	Id        int64 // 0 when Duplicate
	Duplicate bool  // the idempotency claim already existed
}
