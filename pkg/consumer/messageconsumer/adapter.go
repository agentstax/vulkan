package messageconsumer

import (
	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/consumer/message"
	"github.com/agentstax/vulkan/pkg/consumer/messageconsumer/controller"
	workercontroller "github.com/agentstax/vulkan/pkg/worker/controller"
)

func toMessageConsumerMetadata(cfg *MessageConsumerConfig) *messageConsumerMetadata {
	return &messageConsumerMetadata{
		ClaimPollRate:           workercontroller.NewMetadataValue(cfg.ClaimPollRate),
		MaxRangeReclaims:        workercontroller.NewMetadataValue(cfg.MaxRangeReclaims),
		ExceptionInitialBackoff: workercontroller.NewMetadataValue(cfg.ExceptionInitialBackoff),
		Message:                 workercontroller.NewMetadataValue(*cfg.Message),
		ConcurrencyOverride:     workercontroller.NewMetadataValue(cfg.ConcurrencyOverride),
	}
}

func toMessageMeta(claimed controller.Message, resolved *common.MessageOptions) message.MessageMeta {
	return message.MessageMeta{
		Id:             claimed.Id,
		RoutingKey:     claimed.RoutingKey,
		CompactionKey:  claimed.CompactionKey,
		CompactionRank: claimed.CompactionRank,
		CreatedAt:      claimed.CreatedAt,
		Options:        resolved,
	}
}
