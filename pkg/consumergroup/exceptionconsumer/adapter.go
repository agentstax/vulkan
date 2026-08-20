package exceptionconsumer

import (
	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/consumergroup"
	"github.com/agentstax/vulkan/pkg/consumergroup/exceptionconsumer/controller"
)

func toExceptionConsumerMetadata(cfg *ExceptionConsumerConfig) *exceptionConsumerMetadata {
	return &exceptionConsumerMetadata{
		ClaimPollRate:       cfg.ClaimPollRate,
		Message:             *cfg.Message,
		ConcurrencyOverride: cfg.ConcurrencyOverride,
	}
}

func toExceptionMessageMeta(exception *controller.ClaimedException, resolved *common.MessageOptions) consumergroup.MessageMeta {
	return consumergroup.MessageMeta{
		Id:             exception.MessageId,
		RoutingKey:     exception.RoutingKey,
		CompactionKey:  exception.CompactionKey,
		CompactionRank: exception.CompactionRank,
		CreatedAt:      exception.CreatedAt,
		Options:        resolved,
	}
}
