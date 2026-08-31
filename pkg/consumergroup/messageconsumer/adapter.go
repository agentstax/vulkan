package messageconsumer

import (
	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/consumergroup"
	"github.com/agentstax/vulkan/pkg/consumergroup/messageconsumer/controller"
)

func toMessageConsumerMetadata(cfg *MessageConsumerConfig) *MessageConsumerMetadata {
	return &MessageConsumerMetadata{
		Message:                 cfg.Message,
		MessageMin:              cfg.MessageMin,
		MessageMax:              cfg.MessageMax,
		ConcurrencyOverride:     cfg.ConcurrencyOverride,
		ExceptionInitialBackoff: cfg.ExceptionInitialBackoff,
		MaxRangeReclaims:        cfg.MaxRangeReclaims,
	}
}

func toMessageMeta(claimed controller.Message, resolved *common.MessageOptions) consumergroup.MessageMeta {
	return consumergroup.MessageMeta{
		Id:             claimed.Id,
		RoutingKey:     claimed.RoutingKey,
		MessageKey:     claimed.MessageKey,
		CompactionRank: claimed.CompactionRank,
		CreatedAt:      claimed.CreatedAt,
		ScheduledAt:    resolved.ScheduledAt,
		Options:        resolved,
	}
}
