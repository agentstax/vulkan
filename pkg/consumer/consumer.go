package consumer

import (
	"context"
	"errors"
	"fmt"
	"time"

	"golang.org/x/sync/errgroup"
)

// fuck options patterns it always sucks to me
// long live dysfunctional options pattern - https://rednafi.com/go/dysfunctional-options-pattern/

// ideally idepotent func
type ConsumerFunc[WorkType any] func(ctx context.Context, work *WorkType) error

type Consumer[WorkType any] interface {
	Consume(ctx context.Context, consumerFunc ConsumerFunc[WorkType]) error
}

type WorkConsumerConfig struct {
	BatchLimit      int
	PollRate        time.Duration
	ShutdownTimeout time.Duration
}

// TODO - abstract lifecycle funcs like startup -> pull(poll) -> shutdown into a Lifecycle struct with overridable values
type WorkConsumer[WorkType any] struct {
	Datastore    Datastore[WorkType]
	ShutdownFunc ShutdownFunc[WorkType]
	Config       *WorkConsumerConfig
}

// only required params here
func NewWorkConsumer[WorkType any](datastore Datastore[WorkType]) *WorkConsumer[WorkType] {
	return &WorkConsumer[WorkType]{
		Datastore:    datastore,
		ShutdownFunc: DefaultShutdownFunc[WorkType],
		Config: &WorkConsumerConfig{
			BatchLimit:      1, // no batching by default
			PollRate:        5 * time.Second,
			ShutdownTimeout: 10 * time.Second,
		},
	}
}

func (p *WorkConsumer[WorkType]) Consume(ctx context.Context, consumerFunc ConsumerFunc[WorkType]) error {
	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		fmt.Println("consumer starting")
		return p.Poll(ctx, consumerFunc)
	})

	// block / wait for error
	errWg := g.Wait()
	if errors.Is(errWg, context.Canceled) {
		errWg = nil // requested shutdown, not a failure
	}

	// always attempt to gracefully shutdown

	errShutdown := p.Shutdown(ctx)

	// any nil errors are discarded (so both nil -> returns nil)
	return errors.Join(errWg, errShutdown)
}

func (p *WorkConsumer[WorkType]) Poll(ctx context.Context, consumerFunc ConsumerFunc[WorkType]) error {
	ticker := time.NewTicker(p.Config.PollRate)
	defer ticker.Stop()

	for {
		select {
		// wait for shutdown signal like SIGINT (Ctrl+C) and SIGTERM (docker stop, kill)
		case <-ctx.Done():
			fmt.Println("consumer stopping")
			return ctx.Err()
		case <-ticker.C:
			p.ProcessBatch(ctx, consumerFunc)
		}
	}
}

func (p *WorkConsumer[WorkType]) ProcessBatch(ctx context.Context, consumerFunc ConsumerFunc[WorkType]) {
	// work should not be immediately cancelled on a SIGINT/SIGTERM
	// instead attempt to finish inflight requests bounded by timeout
	// TODO - timeout should be configurable
	batchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)

	// TODO - need to consider how timeout and retry logic works together
	// each attempt should have its own timeout, but could be easy to mess up
	defer cancel()

	err := p.Datastore.ProcessMessages(batchCtx, p.Config.BatchLimit, consumerFunc)
	if err != nil {
		// processing errors should not cancel thread
		// TODO - should have retry and terminal failure logic here
		fmt.Printf("processing error (will retry next poll): %v\n", err)
	}
}

func (p *WorkConsumer[WorkType]) Shutdown(ctx context.Context) error {
	// graceful shutdown:
	// - cannot pass cancel context otherwise any functionality that uses ctx will immediately fail which is not what we want
	// - need to pass timeout as well so shutdown cannot hang forever
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), p.Config.ShutdownTimeout)
	defer cancel()

	return p.ShutdownFunc(shutdownCtx, p)
}
