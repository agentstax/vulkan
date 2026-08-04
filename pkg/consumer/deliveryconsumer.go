package consumer

// The LIFECYCLE consumption path, PARKED: at the current feature set it is a
// strictly more expensive CURSOR (a delivery row per message vs one frontier)
// with no shipped capability CURSOR lacks. It re-earns its place only with the
// non-FIFO queue work (priority/delay/fairness -- see TODO.md's lifecycle
// entries). Keep its labs green; don't invest new work here.

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	consumermetrics "github.com/agentstax/vulkan/pkg/consumer/metrics"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/worker"
	workercontroller "github.com/agentstax/vulkan/pkg/worker/controller"
	"golang.org/x/sync/errgroup"
)

type DeliveryConsumerDefinition[Message any] struct {
	Config *ConsumerConfig
	Logger logger.Logger

	ds              *datastore.PostgresDatastore
	workers         *workercontroller.WorkerController
	abandonedEvents *consumermetrics.MetricEventProducer
	consumerFunc    ConsumerFunc[Message]
}

func NewDeliveryConsumerDefinition[Message any](ds *datastore.PostgresDatastore, consumerFunc ConsumerFunc[Message], abandonedEvents *consumermetrics.MetricEventProducer, cfg *ConsumerConfig) (*DeliveryConsumerDefinition[Message], error) {
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}
	if consumerFunc == nil {
		return nil, errors.New("consumerFunc must not be nil")
	}
	if abandonedEvents == nil {
		return nil, errors.New("abandonedEvents producer must not be nil")
	}
	if cfg == nil {
		cfg = &ConsumerConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	workers, err := workercontroller.NewWorkerController(ds, &workercontroller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &DeliveryConsumerDefinition[Message]{
		Config:          cfg,
		Logger:          cfg.Logger,
		ds:              ds,
		workers:         workers,
		abandonedEvents: abandonedEvents,
		consumerFunc:    consumerFunc,
	}, nil
}

func (f *DeliveryConsumerDefinition[Message]) Name() string {
	return WorkerDeliveryConsumer
}

func (f *DeliveryConsumerDefinition[Message]) Declare(ctx context.Context, owner *common.Owner) error {
	return declareConsumerWorker(ctx, f.workers, WorkerDeliveryConsumer, owner)
}

// a nil Execution is a declined claim, not an error -- try again later.
func (f *DeliveryConsumerDefinition[Message]) Provision(ctx context.Context, workerId int64, owner *common.Owner, metadata any) (worker.Execution, error) {
	claimed, _, err := workercontroller.RegisterInstance[consumerWorkerMetadata](ctx, f.workers, workerId, owner, common.OwnerConsumerGroup, WorkerDeliveryConsumer, metadata, f.Config.InstanceTTL)
	if err != nil || claimed == nil {
		return nil, err
	}

	return newDeliveryConsumerExecution(ctx, f, owner, claimed)
}

type DeliveryConsumerExecution[Message any] struct {
	*consumerBase[Message]

	instanceRunner *workercontroller.InstanceRunner
	deliveryRunner *deliveryRunner[Message]
}

func newDeliveryConsumerExecution[Message any](ctx context.Context, definition *DeliveryConsumerDefinition[Message], owner *common.Owner, claimed *worker.WorkerInstance) (*DeliveryConsumerExecution[Message], error) {
	if definition == nil {
		return nil, errors.New("definition must not be nil")
	}
	if owner == nil {
		return nil, errors.New("owner must not be nil")
	}
	if claimed == nil {
		return nil, errors.New("claimed worker instance must not be nil")
	}

	base, err := newConsumerBase(ctx, definition.ds, owner, definition.consumerFunc, definition.abandonedEvents, definition.Config)
	if err != nil {
		return nil, err
	}
	deliveryRunner, err := newDeliveryRunner(base)
	if err != nil {
		return nil, err
	}
	instanceRunner, err := workercontroller.NewInstanceRunner(definition.workers, claimed, &workercontroller.InstanceRunnerConfig{
		InstanceTTL: definition.Config.InstanceTTL,
		Logger:      logger.With(definition.Logger, "worker", WorkerDeliveryConsumer, "owner", owner.Name),
	})
	if err != nil {
		return nil, err
	}

	return &DeliveryConsumerExecution[Message]{
		consumerBase:   base,
		instanceRunner: instanceRunner,
		deliveryRunner: deliveryRunner,
	}, nil
}

func (i *DeliveryConsumerExecution[Message]) Run(ctx context.Context) error {
	return i.instanceRunner.Run(ctx, i.deliveryRunner.run)
}

type deliveryRunner[Message any] struct {
	*consumerBase[Message]
}

func newDeliveryRunner[Message any](base *consumerBase[Message]) (*deliveryRunner[Message], error) {
	if base == nil {
		return nil, errors.New("base must not be nil")
	}
	return &deliveryRunner[Message]{consumerBase: base}, nil
}

func (r *deliveryRunner[Message]) run(ctx context.Context) error {
	g, gCtx := errgroup.WithContext(ctx)
	g.Go(func() error {
		return r.project(gCtx)
	})
	g.Go(func() error {
		return r.processDeliveries(gCtx)
	})
	return g.Wait()
}

func (r *deliveryRunner[Message]) project(ctx context.Context) error {
	ticker := time.NewTicker(r.Config.ClaimPollRate)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := r.Datastore.FanOut(ctx, r.Topic.Id, r.Group.Id, r.Config.FanOutBatchLimit); err != nil {
				return err
			}
		}
	}
}

func (r *deliveryRunner[Message]) processDeliveries(ctx context.Context) error {
	ticker := time.NewTicker(r.Config.ClaimPollRate)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := r.deliveryClaim(ctx); err != nil {
				return err
			}
		}
	}
}

// each delivery resolves on its own, so one dead-lettered message never stops
// the batch behind it -- the isolation the cursor path can't give.
//
// No lease handling: this path never grew crash recovery, so a delivery left in
// 'processing' by a consumer that died mid-run just sits there.
func (r *deliveryRunner[Message]) deliveryClaim(ctx context.Context) error {
	deliveries, err := r.Datastore.ClaimMessagesWithLifecycle(ctx, r.Topic.Id, r.Group.Id, r.Config.BatchLimit)
	if err != nil {
		return err
	}

	for _, delivery := range deliveries {
		var message Message
		if err := json.Unmarshal(delivery.Payload, &message); err != nil {
			// a bad payload will never deserialize -> straight to the DLQ, no retries
			if recordErr := r.Datastore.RecordTerminal(ctx, &delivery, err, r.Topic.DisableDeliveryLog); recordErr != nil {
				return recordErr
			}
			continue
		}

		resolvedOptions := r.Config.resolveMessageOptions(delivery.Options)
		if err := r.callSafely(ctx, r.consumerFunc, &message, delivery.MessageId, delivery.Attempts, delivery.Options, resolvedOptions.Timeout); err != nil {
			// processing error -> retry until attempts exhaust, then dead-letter
			if recordErr := r.Datastore.RecordFailure(ctx, resolvedOptions.Retry.MaxRetries, &delivery, err, r.Topic.DisableDeliveryLog); recordErr != nil {
				return recordErr
			}
			continue
		}

		if err := r.Datastore.RecordSuccess(ctx, &delivery); err != nil {
			return err
		}
	}

	return nil
}
