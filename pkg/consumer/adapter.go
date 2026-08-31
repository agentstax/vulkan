package consumer

import (
	"github.com/agentstax/vulkan/pkg/consumergroup/exceptionconsumer"
	"github.com/agentstax/vulkan/pkg/consumergroup/messageconsumer"
)

// Each worker row runs on its own slice of the group config and the session
// options. Both arrive resolved -- cfg by NewConsumer, options by Consume --
// so the row configs carry resolved values and their own WithDefaults is a
// no-op.

func toMessageConsumerConfig(cfg *ConsumerConfig, options *ConsumeOptions) *messageconsumer.MessageConsumerConfig {
	return &messageconsumer.MessageConsumerConfig{
		BatchLimit:              options.BatchLimit,
		QueueSize:               options.QueueSize,
		MessageConcurrency:      options.MessageConcurrency,
		MaxRangeReclaims:        cfg.MaxRangeReclaims,
		ClaimPollRate:           options.ClaimPollRate,
		QueueMargin:             options.QueueMargin,
		RecordMargin:            options.RecordMargin,
		TimeoutGrace:            options.TimeoutGrace,
		SlowDispatchThreshold:   options.SlowDispatchThreshold,
		ExceptionInitialBackoff: cfg.ExceptionInitialBackoff,
		ShutdownTimeout:         options.ShutdownTimeout,
		InstanceTTL:             options.InstanceTTL,
		Message:                 cfg.Message,
		MessageMin:              cfg.MessageMin,
		MessageMax:              cfg.MessageMax,
		ConcurrencyOverride:     cfg.ConcurrencyOverride,
		Logger:                  cfg.Logger,
		Retry:                   cfg.Retry,
	}
}

func toExceptionConsumerConfig(cfg *ConsumerConfig, options *ConsumeOptions) *exceptionconsumer.ExceptionConsumerConfig {
	return &exceptionconsumer.ExceptionConsumerConfig{
		BatchLimit:            options.BatchLimit,
		ClaimPollRate:         options.ClaimPollRate,
		QueueMargin:           options.QueueMargin,
		RecordMargin:          options.RecordMargin,
		TimeoutGrace:          options.TimeoutGrace,
		SlowDispatchThreshold: options.SlowDispatchThreshold,
		InstanceTTL:           options.InstanceTTL,
		Message:               cfg.Message,
		MessageMin:            cfg.MessageMin,
		MessageMax:            cfg.MessageMax,
		ConcurrencyOverride:   cfg.ConcurrencyOverride,
		Logger:                cfg.Logger,
		Retry:                 cfg.Retry,
	}
}
