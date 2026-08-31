package controller

import (
	"encoding/json"
	"github.com/agentstax/vulkan/pkg/topic"
	"uuid"

	"github.com/agentstax/vulkan/pkg/producer/controller/datastore"
)

func toAppend[Message topic.Versioned](idempotencyKey uuid.UUID, payload *Message, options ProduceOptions) *datastore.Append[Message] {
	data := &datastore.Append[Message]{
		IdempotencyKey: idempotencyKey,
		Payload:        payload,
		RoutingKey:     options.RoutingKey,
		MessageKey:     options.MessageKey,
		Options:        options.Message,
	}
	if options.Compaction != nil && options.Compaction.Enable {
		data.Compacted = true
		data.CompactionRank = options.Compaction.Rank
	}
	return data
}

func toAppended[Message topic.Versioned](data *datastore.Appended[Message]) *Appended[Message] {
	return &Appended[Message]{
		Message:   data.Message,
		Id:        data.Id,
		Duplicate: data.Duplicate,
	}
}

func toMessageData[Message topic.Versioned](data *datastore.MessageLogRow) (*MessageData[Message], error) {
	var message Message
	if err := json.Unmarshal(data.Payload, &message); err != nil {
		return nil, err
	}
	return &MessageData[Message]{
		Id:             data.Id,
		Message:        &message,
		CreatedAt:      data.CreatedAt,
		RoutingKey:     data.RoutingKey,
		MessageKey:     data.MessageKey,
		CompactionRank: data.CompactionRank,
	}, nil
}
