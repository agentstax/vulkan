package exceptionconsumer

import (
	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/consumer/exceptionconsumer/controller"
	"github.com/agentstax/vulkan/pkg/consumer/message"
)

func toExceptionConsumerMetadata(cfg *ExceptionConsumerConfig) *exceptionConsumerMetadata {
	return &exceptionConsumerMetadata{
		ClaimPollRate:       cfg.ClaimPollRate,
		Message:             *cfg.Message,
		ConcurrencyOverride: cfg.ConcurrencyOverride,
	}
}

func toExceptionMessageMeta(exception *controller.ClaimedException, resolved *common.MessageOptions) message.MessageMeta {
	return message.MessageMeta{
		Id:             exception.MessageId,
		RoutingKey:     exception.RoutingKey,
		CompactionKey:  exception.CompactionKey,
		CompactionRank: exception.CompactionRank,
		CreatedAt:      exception.CreatedAt,
		Options:        resolved,
	}
}
