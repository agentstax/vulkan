package controller

import (
	"github.com/agentstax/vulkan/pkg/common"
)

// Appended is one append's outcome.
type Appended[Message common.Versioned] struct {
	// Message - the payload this call built. On a duplicate it is NOT the
	// originally-stored payload: the idempotency table records only the key.
	Message *Message

	Id        int64 // 0 when Duplicate
	Duplicate bool  // the idempotency claim already existed
}
