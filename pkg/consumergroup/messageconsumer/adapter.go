package messageconsumer

import (
	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/consumergroup"
	"github.com/agentstax/vulkan/pkg/consumergroup/messageconsumer/controller"
)

func toMessageConsumerMetadata(cfg *MessageConsumerConfig) *messageConsumerMetadata {
	return &messageConsumerMetadata{
		ClaimPollRate:           cfg.ClaimPollRate,
		MaxRangeReclaims:        cfg.MaxRangeReclaims,
		ExceptionInitialBackoff: cfg.ExceptionInitialBackoff,
		Message:                 *cfg.Message,
		ConcurrencyOverride:     cfg.ConcurrencyOverride,
	}
}

func toMessageMeta(claimed controller.Message, resolved *common.MessageOptions) consumergroup.MessageMeta {
	return consumergroup.MessageMeta{
		Id:             claimed.Id,
		RoutingKey:     claimed.RoutingKey,
		CompactionKey:  claimed.CompactionKey,
		CompactionRank: claimed.CompactionRank,
		CreatedAt:      claimed.CreatedAt,
		Options:        resolved,
	}
}
