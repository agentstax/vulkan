package consumer

import (
	"context"
	"errors"
	"fmt"
	"time"

	"golang.org/x/sync/errgroup"
)

type ConsumerFunc[WorkType any] func(ctx context.Context, work WorkType) error

type Consumer[WorkType any] interface {
	Consume(ctx context.Context, consumerFunc ConsumerFunc[WorkType]) error
}

type WorkConsumer[WorkType any] struct {
	Datastore  Datastore[WorkType]
	BatchLimit int
	PollRate   time.Duration
}

func NewWorkConsumer[WorkType any](datastore Datastore[WorkType], batchLimit int, pollRate time.Duration) (*WorkConsumer[WorkType], error) {
	return &WorkConsumer[WorkType]{
		Datastore:  datastore,
		BatchLimit: batchLimit,
		PollRate:   pollRate,
	}, nil
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
	ticker := time.NewTicker(p.PollRate)
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

	err := p.Datastore.ProcessMessages(batchCtx, p.BatchLimit, consumerFunc)
	if err != nil {
		// processing errors should not cancel thread
		// TODO - should have retry and terminal failure logic here
		fmt.Printf("processing error (will retry next poll): %v\n", err)
	}
}

func (p *WorkConsumer[WorkType]) Shutdown(ctx context.Context) error {
	if err := p.Datastore.Shutdown(ctx); err != nil {
		return err
	}

	// simulate long running shutdown for now
	time.Sleep(3 * time.Second)

	return nil
}
