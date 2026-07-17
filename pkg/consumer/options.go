package consumer

import "time"

func (c *MessageConsumer[Message]) WithType(consumerType ConsumerType) *MessageConsumer[Message] {
	c.Config.Type = consumerType
	return c
}

func (c *MessageConsumer[Message]) WithBatchLimit(batchLimit int) *MessageConsumer[Message] {
	c.Config.BatchLimit = batchLimit
	return c
}

func (c *MessageConsumer[Message]) WithMaxAttempts(maxAttempts int) *MessageConsumer[Message] {
	c.Config.MaxAttempts = maxAttempts
	return c
}

func (c *MessageConsumer[Message]) WithWorkTimeout(workTimeout time.Duration) *MessageConsumer[Message] {
	c.Config.WorkTimeout = workTimeout
	return c
}

func (c *MessageConsumer[Message]) WithQueueMargin(queueMargin time.Duration) *MessageConsumer[Message] {
	c.Config.QueueMargin = queueMargin
	return c
}

func (c *MessageConsumer[Message]) WithAckMargin(ackMargin time.Duration) *MessageConsumer[Message] {
	c.Config.AckMargin = ackMargin
	return c
}

func (c *MessageConsumer[Message]) WithClaimPollRate(claimPollRate time.Duration) *MessageConsumer[Message] {
	c.Config.ClaimPollRate = claimPollRate
	return c
}
