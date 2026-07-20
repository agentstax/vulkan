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
	"time"
)

func (p *MessageConsumer[Message]) Project(ctx context.Context) error {
	if p.Config.Type == CURSOR {
		return nil // don't need projection for cursor only
	}

	ticker := time.NewTicker(p.Config.ClaimPollRate)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := p.Datastore.FanOut(ctx, p.Topic.Id, p.Group, p.Config.FanOutBatchLimit); err != nil {
				return err
			}
		}
	}
}

// LifecycleClaim is the per-row lifecycle path (Phase 6): claim this group's own
// delivery rows and run each through the Phase 2 state machine
// (success -> 'done', retryable failure -> 'ready', exhausted/bad payload -> 'dead').
//
// Unlike CursorClaim, a single message's failure does NOT stop the batch: each
// delivery resolves independently, so group A can dead-letter message 5 while it
// keeps draining 6, 7, 8. That per-message isolation is the whole point of the
// delivery table -- the cursor model can't do it (one bad message blocks the line).
//
// No lease handling here: Phase 6 doesn't do crash recovery, so a delivery left in
// 'processing' (consumer died mid-process) just sits there until Phase 6.5's reclaim.
func (p *MessageConsumer[Message]) LifecycleClaim(ctx context.Context, consumerFunc ConsumerFunc[Message]) error {
	deliveries, err := p.Datastore.ClaimMessagesWithLifecycle(ctx, p.Topic.Id, p.Group, p.Config.BatchLimit)
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
