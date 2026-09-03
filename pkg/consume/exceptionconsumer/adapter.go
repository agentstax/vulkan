package exceptionconsumer

import (
	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/consume"
	"github.com/agentstax/vulkan/pkg/consume/exceptionconsumer/controller"
)

func toExceptionConsumerMetadata(cfg *ExceptionConsumerConfig) *ExceptionConsumerMetadata {
	return &ExceptionConsumerMetadata{
		Message:             cfg.Message,
		MessageMin:          cfg.MessageMin,
		MessageMax:          cfg.MessageMax,
		ConcurrencyOverride: cfg.ConcurrencyOverride,
	}
}

func toExceptionMessageMeta(exception *controller.ClaimedException, resolved *common.MessageOptions) consume.MessageMeta {
	return consume.MessageMeta{
		Id:             exception.MessageId,
		RoutingKey:     exception.RoutingKey,
		MessageKey:     exception.MessageKey,
		CompactionRank: exception.CompactionRank,
		CreatedAt:      exception.CreatedAt,
		ScheduledAt:    resolved.ScheduledAt,
		Attempts:       exception.Attempts,
		Delays:         exception.Delays,
		Options:        resolved,
	}
}
