package consumer

import (
	"time"

	"github.com/agentstax/vulkan/pkg/retry"
)

// No WithType: NewConsumer builds a different consumer per Config.Type, so
// the type can't change after construction -- set it in the config.

func (c *Consumer[Message]) WithBatchLimit(batchLimit int) *Consumer[Message] {
	c.Config.BatchLimit = batchLimit
	return c
}

func (c *Consumer[Message]) WithTimeout(timeout time.Duration) *Consumer[Message] {
	c.Config.Message.Timeout = timeout
	return c
}

func (c *Consumer[Message]) WithQueueMargin(queueMargin time.Duration) *Consumer[Message] {
	c.Config.QueueMargin = queueMargin
	return c
}

func (c *Consumer[Message]) WithAckMargin(ackMargin time.Duration) *Consumer[Message] {
	c.Config.AckMargin = ackMargin
	return c
}

func (c *Consumer[Message]) WithClaimPollRate(claimPollRate time.Duration) *Consumer[Message] {
	c.Config.ClaimPollRate = claimPollRate
	return c
}

func (c *Consumer[Message]) WithMessageRetry(policy *retry.Policy) *Consumer[Message] {
	c.Config.Message.Retry = policy
	return c
}
