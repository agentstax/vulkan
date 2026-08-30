package controller

import (
	"encoding/json"

	"github.com/agentstax/vulkan/pkg/producer/controller/datastore"
	"github.com/google/uuid"
)

func toAppendData[Message any](idempotencyKey uuid.UUID, payload *Message, schemaVersion int64, options ProduceOptions) *datastore.AppendData[Message] {
	data := &datastore.AppendData[Message]{
		IdempotencyKey: idempotencyKey,
		SchemaVersion:  schemaVersion,
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
	return &MessageRow[Message]{
		Id:             data.Id,
		Message:        &message,
		CreatedAt:      data.CreatedAt,
		RoutingKey:     data.RoutingKey,
		MessageKey:     data.MessageKey,
		CompactionRank: data.CompactionRank,
	}, nil
}
