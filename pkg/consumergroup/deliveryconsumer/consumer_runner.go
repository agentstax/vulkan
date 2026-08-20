package deliveryconsumer

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	consumerbase "github.com/agentstax/vulkan/pkg/consumergroup/base"
	"github.com/agentstax/vulkan/pkg/consumergroup/deliveryconsumer/controller"
	"golang.org/x/sync/errgroup"
)

type deliveryRunner[Message any] struct {
	*consumerbase.BaseConsumer[Message]

	consumers *controller.DeliveryConsumerGroupController
	cfg       *DeliveryConsumerConfig
}

func newDeliveryRunner[Message any](base *consumerbase.BaseConsumer[Message], consumers *controller.DeliveryConsumerGroupController, cfg *DeliveryConsumerConfig) (*deliveryRunner[Message], error) {
	if base == nil {
		return nil, errors.New("base must not be nil")
	}
	if consumers == nil {
		return nil, errors.New("consumers controller must not be nil")
	}
	if cfg == nil {
		return nil, errors.New("config must not be nil")
	}

	return &deliveryRunner[Message]{BaseConsumer: base, consumers: consumers, cfg: cfg}, nil
}

func (r *deliveryRunner[Message]) run(ctx context.Context) error {
	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() error {
		return r.project(groupCtx)
	})
	group.Go(func() error {
		return r.processDeliveries(groupCtx)
	})
	return group.Wait()
}

func (r *deliveryRunner[Message]) project(ctx context.Context) error {
	ticker := time.NewTicker(r.cfg.ClaimPollRate)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := r.consumers.FanOut(ctx, r.Topic.Id, r.Owner.ConsumerGroupId, r.cfg.FanOutBatchLimit); err != nil {
				return err
			}
		}
	}
}

func (r *deliveryRunner[Message]) processDeliveries(ctx context.Context) error {
	ticker := time.NewTicker(r.cfg.ClaimPollRate)
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
	deliveries, err := r.consumers.ClaimMessagesWithLifecycle(ctx, r.Topic.Id, r.Owner.ConsumerGroupId, r.cfg.BatchLimit)
	if err != nil {
		return err
	}

	for _, delivery := range deliveries {
		var payload Message
		if err := json.Unmarshal(delivery.Payload, &payload); err != nil {
			// a bad payload will never deserialize -> straight to the DLQ, no retries
			if recordErr := r.consumers.RecordTerminal(ctx, &delivery, err, r.Topic.DeliveryLogMode); recordErr != nil {
				return recordErr
			}
			continue
		}

		resolvedOptions := r.cfg.resolveMessageOptions(delivery.Options)
		if err := r.CallSafely(ctx, &payload, delivery.MessageId, delivery.Attempts, delivery.Options, resolvedOptions.Timeout); err != nil {
			// processing error -> retry until attempts exhaust, then dead-letter
			if recordErr := r.consumers.RecordFailure(ctx, resolvedOptions.Retry.MaxRetries, &delivery, err, r.Topic.DeliveryLogMode); recordErr != nil {
				return recordErr
			}
			continue
		}

		if err := r.consumers.RecordSuccess(ctx, &delivery, r.Topic.DeliveryLogMode); err != nil {
			return err
		}
	}

	return nil
}
