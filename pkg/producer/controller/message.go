package controller

import (
	"errors"
	"time"
)

// TODO - this might need to be a common type,
// it differs from consumer.MessageRow by Message vs Payload (raw json)
type MessageRow[Message any] struct {
	Id             int64
	Message        *Message
	CreatedAt      time.Time
	RoutingKey     string
	CompactionKey  string
	CompactionRank int64
}

func NewMessageRow[Message any](id int64, message *Message, createdAt time.Time, routingKey, compactionKey string, compactionRank int64) (*MessageRow[Message], error) {
	if message == nil {
		return nil, errors.New("message must not be nil")
	}
	return &MessageRow[Message]{
		Message:        message,
		Id:             id,
		CreatedAt:      createdAt,
		RoutingKey:     routingKey,
		CompactionKey:  compactionKey,
		CompactionRank: compactionRank,
	}, nil
}

// Appended is one append's outcome.
type Appended[Message any] struct {
	// Message - the payload this call built. On a duplicate it is NOT the
	// originally-stored payload: the idempotency table records only the key.
	Message *Message

	Id        int64 // 0 when Duplicate
	Duplicate bool  // the idempotency claim already existed
}
