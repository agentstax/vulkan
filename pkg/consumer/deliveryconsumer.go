package consumer

// The LIFECYCLE consumption path, PARKED: at the current feature set it is a
// strictly more expensive CURSOR (a delivery row per message vs one frontier)
// with no shipped capability CURSOR lacks. It re-earns its place only with the
// non-FIFO queue work (priority/delay/fairness -- see TODO.md's lifecycle
// entries). Keep its labs green; don't invest new work here.
//
// Datastore half lives in datastore_lifecycle.go.

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/agentstax/vulkan/pkg/consumer/metrics"
	"github.com/agentstax/vulkan/pkg/datastore"
	vulkanerrors "github.com/agentstax/vulkan/pkg/errors"
	"github.com/agentstax/vulkan/pkg/maintain"
	"github.com/agentstax/vulkan/pkg/topic"
	"golang.org/x/sync/errgroup"
)

// DeliveryConsumer is the per-delivery-row work loop: fan each message out
// into the group's own delivery rows, claim them, and resolve each one
// independently through consumerFunc.
type DeliveryConsumer[Message any] struct {
	*consumerBase[Message]

	maintenance *maintain.MaintenanceDatastore // cold-start partition create only, never duty work
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewDeliveryConsumer[Message any](consumerGroup string, topicName string, version topic.SchemaVersion, ds *datastore.PostgresDatastore, cfg *ConsumerConfig) (*DeliveryConsumer[Message], error) {
	if topicName == "" {
		return nil, errors.New("topic name is required")
	}
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}

	if cfg == nil {
		cfg = &ConsumerConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	base, err := newConsumerBase[Message](consumerGroup, topicName, version, ds, cfg)
	if err != nil {
		return nil, err
	}

	maintenanceDatastore, err := maintain.NewMaintenanceDatastore(ds, &maintain.MaintenanceDatastoreConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &DeliveryConsumer[Message]{
		consumerBase: base,
		maintenance:  maintenanceDatastore,
	}, nil
}

// Register resolves this consumer's topic by name against the live topic row,
// sets up its cursor, and starts the consumer's lifecycle.
//
// ctx must be cancellable, unless ConsumerConfig.DisableGracefulShutdown
// declares otherwise.
func (p *DeliveryConsumer[Message]) Register(ctx context.Context) error {
	if err := p.register(ctx); err != nil {
		return err
	}

	// AbandonedRoutines only: the queue-state gauges read cursor claim
	// frontiers this path doesn't drive.
	abandoned, err := metrics.NewAbandonedRoutines(p.Config.Meter, p.consumerGroup, p.Topic.Name, int64(p.version))
	if err != nil {
		return err
	}
	p.Metrics = &metrics.ConsumerMetrics{AbandonedRoutines: abandoned}

	// cold-start guarantee: the next partition exists before the janitor
	// duty's first (jittered) tick
	if err := p.maintenance.EnsureNextPartition(ctx, p.Topic.Id, p.Topic.PartitionSize); err != nil {
		return err
	}

	// tracked for graceful shutdown draining / handling
	p.lifecycleCtx = ctx

	return nil
}

// Consume claims and processes deliveries with consumerFunc, blocking until
// stopped: cancel ctx to stop this call, or cancel the context given to
// Register to wind the whole consumer down. A requested stop from either side
// returns nil
func (p *DeliveryConsumer[Message]) Consume(ctx context.Context, consumerFunc ConsumerFunc[Message]) error {
	if err := p.lifecycleErr(); err != nil {
		return err
	}
	runCtx, cancel := p.runCtx(ctx)
	defer cancel()

	p.Logger.InfoContext(runCtx, "delivery consumer starting", "group", p.consumerGroup, "topic", p.Topic.Id, "version", p.version)

	g, gCtx := errgroup.WithContext(runCtx)
	g.Go(func() error {
		return p.project(gCtx)
	})
	g.Go(func() error {
		return p.processDeliveries(gCtx, consumerFunc)
	})

	err := g.Wait()
	if errors.Is(err, context.Canceled) {
		// requested shutdown (either side), not a failure -- log which side asked
		reason := "caller context cancelled"
		if errors.Is(context.Cause(runCtx), vulkanerrors.ErrShutdownRequested) {
			reason = "lifecycle context cancelled"
		}
		p.Logger.InfoContext(ctx, "delivery consumer stopped", "reason", reason, "group", p.consumerGroup, "topic", p.Topic.Id, "version", p.version)
		err = nil
	}
	return err
}

// project materializes the group's delivery rows from the shared message log.
func (p *DeliveryConsumer[Message]) project(ctx context.Context) error {
	ticker := time.NewTicker(p.Config.ClaimPollRate)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := p.Datastore.FanOut(ctx, p.Topic.Id, p.consumerGroup, p.Config.FanOutBatchLimit); err != nil {
				return err
			}
		}
	}
}

func (p *DeliveryConsumer[Message]) processDeliveries(ctx context.Context, consumerFunc ConsumerFunc[Message]) error {
	ticker := time.NewTicker(p.Config.ClaimPollRate)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := p.DeliveryClaim(ctx, consumerFunc); err != nil {
				return err
			}
		}
	}
}

// DeliveryClaim claims this group's own delivery rows and runs each through
// the delivery state machine (success -> 'done', retryable failure -> 'ready',
// exhausted/bad payload -> 'dead').
//
// Unlike the cursor path, a single message's failure does NOT stop the batch: each
// delivery resolves independently, so group A can dead-letter message 5 while it
// keeps draining 6, 7, 8. That per-message isolation is the whole point of the
// delivery table -- the cursor model can't do it (one bad message blocks the line).
//
// No lease handling here: the parked lifecycle path never grew crash recovery,
// so a delivery left in 'processing' (consumer died mid-process) just sits there.
func (p *DeliveryConsumer[Message]) DeliveryClaim(ctx context.Context, consumerFunc ConsumerFunc[Message]) error {
	deliveries, err := p.Datastore.ClaimMessagesWithLifecycle(ctx, p.Topic.Id, p.consumerGroup, p.Config.BatchLimit)
	if err != nil {
		return err
	}

	for _, delivery := range deliveries {
		var work Message
		if err := json.Unmarshal(delivery.Payload, &work); err != nil {
			// a bad payload will never deserialize -> straight to the DLQ, no retries
			if recordErr := p.Datastore.RecordTerminal(ctx, &delivery, err, p.Topic.DisableDeliveryLog); recordErr != nil {
				return recordErr
			}
			continue
		}

		if err := p.callSafely(ctx, consumerFunc, &work, delivery.MessageId, delivery.Attempts); err != nil {
			// processing error -> retry until attempts exhaust, then dead-letter
			if recordErr := p.Datastore.RecordFailure(ctx, p.Config.MaxAttempts, &delivery, err, p.Topic.DisableDeliveryLog); recordErr != nil {
				return recordErr
			}
			continue
		}

		if err := p.Datastore.RecordSuccess(ctx, &delivery); err != nil {
			return err
		}
	}

	return nil
}
