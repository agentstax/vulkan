package consumer

import (
	"github.com/agentstax/vulkan/pkg/consumer/deliveryconsumer"
	"github.com/agentstax/vulkan/pkg/consumer/exceptionconsumer"
	"github.com/agentstax/vulkan/pkg/consumer/messageconsumer"
)

// Each worker row runs on its own slice of this config. WithDefaults and
// Validate have already run by the time these are built, so the row configs
// carry resolved values and their own WithDefaults is a no-op.

func toMessageConsumerConfig(c *ConsumerConfig) *messageconsumer.MessageConsumerConfig {
	return &messageconsumer.MessageConsumerConfig{
		BatchLimit:              c.BatchLimit,
		QueueSize:               c.QueueSize,
		MessageConcurrency:      c.MessageConcurrency,
		MaxRangeReclaims:        c.MaxRangeReclaims,
		ClaimPollRate:           c.ClaimPollRate,
		QueueMargin:             c.QueueMargin,
		RecordMargin:            c.RecordMargin,
		TimeoutGrace:            c.TimeoutGrace,
		ExceptionInitialBackoff: c.ExceptionInitialBackoff,
		ShutdownTimeout:         c.ShutdownTimeout,
		InstanceTTL:             c.InstanceTTL,
		Message:                 c.Message,
		MessageMin:              c.MessageMin,
		MessageMax:              c.MessageMax,
		ConcurrencyOverride:     c.ConcurrencyOverride,
		Logger:                  c.Logger,
		Retry:                   c.Retry,
	}
}

func toExceptionConsumerConfig(c *ConsumerConfig) *exceptionconsumer.ExceptionConsumerConfig {
	return &exceptionconsumer.ExceptionConsumerConfig{
		BatchLimit:          c.BatchLimit,
		ClaimPollRate:       c.ClaimPollRate,
		QueueMargin:         c.QueueMargin,
		RecordMargin:        c.RecordMargin,
		TimeoutGrace:        c.TimeoutGrace,
		InstanceTTL:         c.InstanceTTL,
		Message:             c.Message,
		MessageMin:          c.MessageMin,
		MessageMax:          c.MessageMax,
		ConcurrencyOverride: c.ConcurrencyOverride,
		Logger:              c.Logger,
		Retry:               c.Retry,
	}
}

func toDeliveryConsumerConfig(c *ConsumerConfig) *deliveryconsumer.DeliveryConsumerConfig {
	return &deliveryconsumer.DeliveryConsumerConfig{
		BatchLimit:          c.BatchLimit,
		FanOutBatchLimit:    c.FanOutBatchLimit,
		ClaimPollRate:       c.ClaimPollRate,
		TimeoutGrace:        c.TimeoutGrace,
		InstanceTTL:         c.InstanceTTL,
		Message:             c.Message,
		MessageMin:          c.MessageMin,
		MessageMax:          c.MessageMax,
		ConcurrencyOverride: c.ConcurrencyOverride,
		Logger:              c.Logger,
		Retry:               c.Retry,
	}
}
