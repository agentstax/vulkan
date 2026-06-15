package consumer

import (
	"context"
	"time"
)

// specifically converting WorkType to Message here to be more in line with community standards

type Datastore[Message any] interface {
	ProcessMessages(ctx context.Context, batchLimit int, maxAttempts int, workTimeout time.Duration, consumerFunc ConsumerFunc[Message]) error
	Shutdown(ctx context.Context) error
}
