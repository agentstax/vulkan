package consumer

import (
	"context"
)

type ShutdownFunc[WorkType any] func(ctx context.Context, consumer *WorkConsumer[WorkType]) error

func DefaultShutdownFunc[WorkType any](ctx context.Context, consumer *WorkConsumer[WorkType]) error {
	return consumer.Datastore.Shutdown(ctx)
}

func (c *WorkConsumer[WorkType]) WithCustomShutdown(shutdownFunc ShutdownFunc[WorkType]) *WorkConsumer[WorkType] {
	c.ShutdownFunc = shutdownFunc
	return c
}
