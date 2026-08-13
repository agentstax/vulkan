package controller

import (
	"encoding/json"

	"github.com/agentstax/vulkan/pkg/producer/controller/datastore"
	"github.com/google/uuid"
)

func toAppendData[Message any](idempotencyKey uuid.UUID, payload *Message, options ProduceOptions) *datastore.AppendData[Message] {
	return &datastore.AppendData[Message]{
		IdempotencyKey: idempotencyKey,
		Payload:        payload,
		RoutingKey:     options.RoutingKey,
		CompactionKey:  options.CompactionKey,
		CompactionRank: options.CompactionRank,
		Options:        options.Message,
	}
}

func toAppended[Message any](data *datastore.AppendedData[Message]) *Appended[Message] {
	return &Appended[Message]{
		Message:   data.Message,
		Id:        data.Id,
		Duplicate: data.Duplicate,
	}
}

func toMessageRow[Message any](data *datastore.HeadData) (*MessageRow[Message], error) {
	var message Message
	if err := json.Unmarshal(data.Payload, &message); err != nil {
		return nil, err
	}
	return NewMessageRow(data.Id, &message, data.CreatedAt, data.RoutingKey, data.CompactionKey, data.CompactionRank)
}
