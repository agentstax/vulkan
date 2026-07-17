package consumer

import (
	"context"
	"time"
)

type ShutdownFunc[Message any] func(ctx context.Context, consumer *MessageConsumer[Message]) error

func DefaultShutdownFunc[Message any](ctx context.Context, consumer *MessageConsumer[Message]) error {
	return consumer.Datastore.Shutdown(ctx)
}

func (c *MessageConsumer[Message]) WithShutdown(shutdownFunc ShutdownFunc[Message]) *MessageConsumer[Message] {
	c.ShutdownFunc = shutdownFunc
	return c
}

func (c *MessageConsumer[Message]) WithShutdownTimeout(shutdownTimeout time.Duration) *MessageConsumer[Message] {
	c.Config.ShutdownTimeout = shutdownTimeout
	return c
}
