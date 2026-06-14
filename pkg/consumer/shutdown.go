package consumer

import (
	"context"
	"time"
)

type ShutdownFunc[WorkType any] func(ctx context.Context, consumer *WorkConsumer[WorkType]) error

func DefaultShutdownFunc[WorkType any](ctx context.Context, consumer *WorkConsumer[WorkType]) error {
	return consumer.Datastore.Shutdown(ctx)
}

func (c *WorkConsumer[WorkType]) WithShutdown(shutdownFunc ShutdownFunc[WorkType]) *WorkConsumer[WorkType] {
	c.ShutdownFunc = shutdownFunc
	return c
}

func (c *WorkConsumer[WorkType]) WithShutdownTimeout(shutdownTimeout time.Duration) *WorkConsumer[WorkType] {
	c.Config.ShutdownTimeout = shutdownTimeout
	return c
}
