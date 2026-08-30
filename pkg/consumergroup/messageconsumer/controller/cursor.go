package controller

import (
	"context"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/google/uuid"
)

// ClaimMessagesWithCursor picks up a crashed range (an expired lease) first and
// only claims fresh work from the frontier when there is nothing to reclaim, so
// crashed ranges drain ahead of new work. Returns (nil, nil) when caught up.
func (c *MessageConsumerGroupController) ClaimMessagesWithCursor(ctx context.Context, topicId int64, groupId int64, schemaVersion int64, limit int, maxRangeReclaims int, leaseDuration time.Duration, deliveryLogMode topic.DeliveryLogMode) (*ClaimedRange, error) {
	if topicId <= 0 {
		return nil, fmt.Errorf("topicId must be > 0, got %d", topicId)
	}
	if groupId <= 0 {
		return nil, fmt.Errorf("groupId must be > 0, got %d", groupId)
	}
	if limit < 1 {
		return nil, fmt.Errorf("limit must be >= 1, got %d", limit)
	}
	if maxRangeReclaims < 1 {
		return nil, fmt.Errorf("maxRangeReclaims must be >= 1, got %d", maxRangeReclaims)
	}
	if leaseDuration <= 0 {
		return nil, fmt.Errorf("leaseDuration must be > 0, got %v", leaseDuration)
	}

	data, err := c.datastore.ClaimMessagesWithCursor(ctx, topicId, groupId, schemaVersion, limit, maxRangeReclaims, leaseDuration, deliveryLogMode)
	if err != nil || data == nil {
		return nil, err
	}
	return toClaimedRange(data), nil
}

// Commit frees the range's lease, then records every outcome as a sparse
// delivery row. initialBackoff is how long a freshly written 'ready' row waits before
// ClaimExceptions can pick it up; RecordExceptionFailure's own retry policy
// takes over from there. Returns ErrLeaseLost if the range was reclaimed.
func (c *MessageConsumerGroupController) Commit(ctx context.Context, topicId int64, groupId int64, token uuid.UUID, outcomes []MessageOutcome, initialBackoff time.Duration, deliveryLogMode topic.DeliveryLogMode) error {
	if err := validateCommit(topicId, groupId, outcomes, initialBackoff); err != nil {
		return err
	}

	return c.datastore.Commit(ctx, topicId, groupId, toTokenData(token), toOutcomeData(outcomes), initialBackoff, deliveryLogMode)
}

// PartialCommit narrows a still-open lease to lastProcessed and records whatever
// resolved before an interruption. The lease is not freed -- it expires and is
// reclaimed, handing the untouched suffix to whoever picks it up next.
func (c *MessageConsumerGroupController) PartialCommit(ctx context.Context, topicId int64, groupId int64, token uuid.UUID, lastProcessed int64, outcomes []MessageOutcome, initialBackoff time.Duration, deliveryLogMode topic.DeliveryLogMode) error {
	if err := validateCommit(topicId, groupId, outcomes, initialBackoff); err != nil {
		return err
	}
	if lastProcessed < 0 {
		return fmt.Errorf("lastProcessed must be >= 0, got %d", lastProcessed)
	}

	return c.datastore.PartialCommit(ctx, topicId, groupId, toTokenData(token), lastProcessed, toOutcomeData(outcomes), initialBackoff, deliveryLogMode)
}

// ForceReclaimRange surrenders a range nobody ever started -- unlike
// PartialCommit this expires the WHOLE lease immediately so the next claim can
// pick it straight back up.
func (c *MessageConsumerGroupController) ForceReclaimRange(ctx context.Context, topicId int64, groupId int64, token uuid.UUID) error {
	if topicId <= 0 {
		return fmt.Errorf("topicId must be > 0, got %d", topicId)
	}
	if groupId <= 0 {
		return fmt.Errorf("groupId must be > 0, got %d", groupId)
	}

	return c.datastore.ForceReclaimRange(ctx, topicId, groupId, toTokenData(token))
}

// ***************
// *** HELPERS ***
// ***************

func validateCommit(topicId int64, groupId int64, outcomes []MessageOutcome, initialBackoff time.Duration) error {
	if topicId <= 0 {
		return fmt.Errorf("topicId must be > 0, got %d", topicId)
	}
	if groupId <= 0 {
		return fmt.Errorf("groupId must be > 0, got %d", groupId)
	}
	if initialBackoff < 0 {
		return fmt.Errorf("initialBackoff must be >= 0, got %v", initialBackoff)
	}
	for _, outcome := range outcomes {
		if err := outcome.Kind.Validate(); err != nil {
			return fmt.Errorf("message %d: %w", outcome.MessageId, err)
		}
	}
	return nil
}
