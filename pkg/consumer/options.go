package consumer

import "time"

func (c *WorkConsumer[WorkType]) WithBatchLimit(batchLimit int) *WorkConsumer[WorkType] {
	c.Config.BatchLimit = batchLimit
	return c
}

func (c *WorkConsumer[WorkType]) WithPollRate(pollRate time.Duration) *WorkConsumer[WorkType] {
	c.Config.PollRate = pollRate
	return c
}
