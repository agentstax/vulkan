package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/google/uuid"
)

// Message is one message_log row handed to a consumer, payload included.
type Message struct {
	Id             int64
	Payload        json.RawMessage
	CreatedAt      time.Time
	RoutingKey     string // "" if unset
	CompactionKey  string // "" if unset
	CompactionRank int64
	Options        *common.MessageOptions
}

// a leased window of work -- the messages to process plus the lease that guards
// them. the worker frees the lease (Commit) once the whole range is done; the
// lazy roller then advances committed past it.
type ClaimedRange struct {
	Lease    RangeLease
	Messages []Message
}

// RangeLease guards a claimed (Low, High] window. Token is what every write
// against the range matches on -- a reclaim rotates it, so a stale worker's
// commit matches nothing.
type RangeLease struct {
	Token           uuid.UUID
	ConsumerGroupId int64
	Low             int64
	High            int64
	Until           time.Time
	Reclaims        int
}

// MessageOutcome is one resolved message of a claimed range, written by
// Commit or PartialCommit.
type MessageOutcome struct {
	MessageId int64
	Kind      OutcomeKind
	Err       string
}

// OutcomeKind is how one message of a claimed range resolved.
type OutcomeKind string

const (
	OutcomeException  OutcomeKind = "exception"  // retryable -- parks as 'ready' instead of failing the whole range
	OutcomeTerminal   OutcomeKind = "terminal"   // no retry could ever succeed -- parks straight to 'dead'
	OutcomeSuperseded OutcomeKind = "superseded" // a newer message on its compaction key exists -- log row only, never a delivery row
	OutcomeDeferred   OutcomeKind = "deferred"   // another delivery held its key -- parks 'deferred' for the exception window
)

func (k OutcomeKind) Validate() error {
	switch k {
	case OutcomeException, OutcomeTerminal, OutcomeSuperseded, OutcomeDeferred:
		return nil
	}
	return fmt.Errorf("invalid outcome kind %q", k)
}

// ClaimMessagesWithCursor picks up a crashed range (an expired lease) first and
// only claims fresh work from the frontier when there is nothing to reclaim, so
// crashed ranges drain ahead of new work. Returns (nil, nil) when caught up.
func (c *MessageConsumerController) ClaimMessagesWithCursor(ctx context.Context, topicId int64, groupId int64, limit int, maxRangeReclaims int, leaseDuration time.Duration, disableDeliveryLog bool) (*ClaimedRange, error) {
	if topicId <= 0 {
		return nil, errors.New("topicId must be > 0")
	}
	if groupId <= 0 {
		return nil, errors.New("groupId must be > 0")
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

	data, err := c.datastore.ClaimMessagesWithCursor(ctx, topicId, groupId, limit, maxRangeReclaims, leaseDuration, disableDeliveryLog)
	if err != nil || data == nil {
		return nil, err
	}
	return toClaimedRange(data), nil
}

// Commit frees the range's lease, then records every outcome as a sparse
// delivery row. initialBackoff is how long a freshly parked row waits before
// ClaimExceptions can pick it up; RecordExceptionFailure's own retry policy
// takes over from there. Returns ErrLeaseLost if the range was reclaimed.
func (c *MessageConsumerController) Commit(ctx context.Context, topicId int64, groupId int64, token uuid.UUID, outcomes []MessageOutcome, initialBackoff time.Duration, disableDeliveryLog bool) error {
	if err := validateCommit(topicId, groupId, outcomes, initialBackoff); err != nil {
		return err
	}

	return c.datastore.Commit(ctx, topicId, groupId, toTokenData(token), toOutcomeData(outcomes), initialBackoff, disableDeliveryLog)
}

// PartialCommit narrows a still-open lease to lastProcessed and parks whatever
// resolved before an interruption. The lease is not freed -- it expires and is
// reclaimed, handing the untouched suffix to whoever picks it up next.
func (c *MessageConsumerController) PartialCommit(ctx context.Context, topicId int64, groupId int64, token uuid.UUID, lastProcessed int64, outcomes []MessageOutcome, initialBackoff time.Duration, disableDeliveryLog bool) error {
	if err := validateCommit(topicId, groupId, outcomes, initialBackoff); err != nil {
		return err
	}
	if lastProcessed < 0 {
		return fmt.Errorf("lastProcessed must be >= 0, got %d", lastProcessed)
	}

	return c.datastore.PartialCommit(ctx, topicId, groupId, toTokenData(token), lastProcessed, toOutcomeData(outcomes), initialBackoff, disableDeliveryLog)
}

// ForceReclaimRange surrenders a range nobody ever started -- unlike
// PartialCommit this expires the WHOLE lease immediately so the next claim can
// pick it straight back up.
func (c *MessageConsumerController) ForceReclaimRange(ctx context.Context, groupId int64, token uuid.UUID) error {
	if groupId <= 0 {
		return errors.New("groupId must be > 0")
	}

	return c.datastore.ForceReclaimRange(ctx, groupId, toTokenData(token))
}

func validateCommit(topicId int64, groupId int64, outcomes []MessageOutcome, initialBackoff time.Duration) error {
	if topicId <= 0 {
		return errors.New("topicId must be > 0")
	}
	if groupId <= 0 {
		return errors.New("groupId must be > 0")
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
