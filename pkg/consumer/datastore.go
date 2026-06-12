package consumer

import "context"

// specifically converting WorkType to Message here to be more in line with community standards

type Datastore[Message any] interface {
	ProcessMessages(ctx context.Context, limit int, consumerFunc ConsumerFunc[Message]) error
	Shutdown(ctx context.Context) error
}
