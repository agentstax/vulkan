package controller

import (
	"encoding/json"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/compaction/controller/datastore"
	"github.com/agentstax/vulkan/pkg/topic"
)

func toMessageRow[Message topic.Versioned](data *datastore.MessageData) (*common.MessageRow[Message], error) {
	var message Message
	if err := json.Unmarshal(data.Payload, &message); err != nil {
		return nil, err
	}
	return &common.MessageRow[Message]{
		Id:             data.Id,
		Message:        &message,
		CreatedAt:      data.CreatedAt,
		RoutingKey:     data.RoutingKey,
		MessageKey:     data.MessageKey,
		CompactionRank: data.CompactionRank,
	}, nil
}
