package consumer

import "time"

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

func (c *WorkConsumer[WorkType]) WithPollRate(pollRate time.Duration) *WorkConsumer[WorkType] {
	c.Config.PollRate = pollRate
	return c
}
