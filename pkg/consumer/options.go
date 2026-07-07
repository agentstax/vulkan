package consumer

import "time"

func (c *WorkConsumer[WorkType]) WithType(consumerType ConsumerType) *WorkConsumer[WorkType] {
	c.Config.Type = consumerType
	return c
}

func (c *WorkConsumer[WorkType]) WithBatchLimit(batchLimit int) *WorkConsumer[WorkType] {
	c.Config.BatchLimit = batchLimit
	return c
}

func (c *WorkConsumer[WorkType]) WithMaxAttempts(maxAttempts int) *WorkConsumer[WorkType] {
	c.Config.MaxAttempts = maxAttempts
	return c
}

func (c *WorkConsumer[WorkType]) WithWorkTimeout(workTimeout time.Duration) *WorkConsumer[WorkType] {
	c.Config.WorkTimeout = workTimeout
	return c
}

func (c *WorkConsumer[WorkType]) WithQueueTimeout(queueTimeout time.Duration) *WorkConsumer[WorkType] {
	c.Config.QueueTimeout = queueTimeout
	return c
}

func (c *WorkConsumer[WorkType]) WithAckMargin(ackMargin time.Duration) *WorkConsumer[WorkType] {
	c.Config.AckMargin = ackMargin
	return c
}

func (c *WorkConsumer[WorkType]) WithClaimPollRate(claimPollRate time.Duration) *WorkConsumer[WorkType] {
	c.Config.ClaimPollRate = claimPollRate
	return c
}

func (c *WorkConsumer[WorkType]) WithJanitorPollRate(janitorPollRate time.Duration) *WorkConsumer[WorkType] {
	c.Config.JanitorPollRate = janitorPollRate
	return c
}
