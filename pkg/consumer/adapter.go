package consumer

import (
	"github.com/agentstax/vulkan/pkg/consumergroup/exceptionconsumer"
	"github.com/agentstax/vulkan/pkg/consumergroup/messageconsumer"
)

// Each worker row runs on its own slice of this config. WithDefaults and
// Validate have already run by the time these are built, so the row configs
// carry resolved values and their own WithDefaults is a no-op.

func toMessageConsumerConfig(cfg *ConsumerConfig) *messageconsumer.MessageConsumerConfig {
	return &messageconsumer.MessageConsumerConfig{
		BatchLimit:              cfg.BatchLimit,
		QueueSize:               cfg.QueueSize,
		MessageConcurrency:      cfg.MessageConcurrency,
		MaxRangeReclaims:        cfg.MaxRangeReclaims,
		ClaimPollRate:           cfg.ClaimPollRate,
		QueueMargin:             cfg.QueueMargin,
		RecordMargin:            cfg.RecordMargin,
		TimeoutGrace:            cfg.TimeoutGrace,
		SlowDispatchThreshold:   cfg.SlowDispatchThreshold,
		ExceptionInitialBackoff: cfg.ExceptionInitialBackoff,
		ShutdownTimeout:         cfg.ShutdownTimeout,
		InstanceTTL:             cfg.InstanceTTL,
		Message:                 cfg.Message,
		MessageMin:              cfg.MessageMin,
		MessageMax:              cfg.MessageMax,
		ConcurrencyOverride:     cfg.ConcurrencyOverride,
		Logger:                  cfg.Logger,
		Retry:                   cfg.Retry,
	}
}

func toExceptionConsumerConfig(cfg *ConsumerConfig) *exceptionconsumer.ExceptionConsumerConfig {
	return &exceptionconsumer.ExceptionConsumerConfig{
		BatchLimit:            cfg.BatchLimit,
		ClaimPollRate:         cfg.ClaimPollRate,
		QueueMargin:           cfg.QueueMargin,
		RecordMargin:          cfg.RecordMargin,
		TimeoutGrace:          cfg.TimeoutGrace,
		SlowDispatchThreshold: cfg.SlowDispatchThreshold,
		InstanceTTL:           cfg.InstanceTTL,
		Message:               cfg.Message,
		MessageMin:            cfg.MessageMin,
		MessageMax:            cfg.MessageMax,
		ConcurrencyOverride:   cfg.ConcurrencyOverride,
		Logger:                cfg.Logger,
		Retry:                 cfg.Retry,
	}
}
