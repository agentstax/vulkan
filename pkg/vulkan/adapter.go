package vulkan

import (
	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/common/logging"
	"github.com/agentstax/vulkan/pkg/consumer"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/producer/batcher"
)

// toProducerConfig copies the declaration onto the producer package's
// config, whose ambient tail the client's own set fills.
func toProducerConfig(cfg *ProducerConfig, retry *common.RetryPolicy, log logging.Logger) *producer.ProducerConfig {
	if cfg == nil {
		cfg = &ProducerConfig{}
	}
	return &producer.ProducerConfig{
		Message: cfg.Message,
		Batch: batcher.BatcherConfig{
			MaxSize:          cfg.Batch.MaxSize,
			ConcurrencyLimit: cfg.Batch.ConcurrencyLimit,
			AttemptTimeout:   cfg.Batch.AttemptTimeout,
			ShutdownGrace:    cfg.Batch.ShutdownGrace,
			Logger:           log,
		},
		SlowProduceThreshold: cfg.SlowProduceThreshold,
		Logger:               log,
		Retry:                retry,
	}
}

// toConsumerConfig copies the declaration onto the consumer package's
// config, whose ambient tail the client's own set fills.
func toConsumerConfig(cfg *ConsumerConfig, retry *common.RetryPolicy, log logging.Logger) *consumer.ConsumerConfig {
	if cfg == nil {
		cfg = &ConsumerConfig{}
	}
	return &consumer.ConsumerConfig{
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
		InstanceTTL:             cfg.InstanceTTL,
		BindingRetryInterval:    cfg.BindingRetryInterval,
		ShutdownTimeout:         cfg.ShutdownTimeout,
		DisableGracefulShutdown: cfg.DisableGracefulShutdown,
		Message:                 cfg.Message,
		MessageMin:              cfg.MessageMin,
		MessageMax:              cfg.MessageMax,
		ConcurrencyOverride:     cfg.ConcurrencyOverride,
		Start:                   cfg.Start,
		Logger:                  log,
		Retry:                   retry,
	}
}
