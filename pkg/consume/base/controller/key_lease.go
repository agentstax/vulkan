package controller

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/agentstax/vulkan/pkg/common"
)

// KeyLeaseVerdict classifies a Claim attempt.
type KeyLeaseVerdict string

const (
	KeyLeaseAcquired   KeyLeaseVerdict = "acquired"   // the caller holds the key until release or expiry
	KeyLeaseBusy       KeyLeaseVerdict = "busy"       // another delivery holds the key
	KeyLeaseSuperseded KeyLeaseVerdict = "superseded" // the compacted message is no longer its key's compaction head -- never run it
)

// RangeBounds is a claimed range (Low, High]; the zero value is no range.
type RangeBounds struct {
	Low  int64
	High int64
}

func NewRangeBounds(low int64, high int64) (RangeBounds, error) {
	if low < 0 {
		return RangeBounds{}, fmt.Errorf("low must be >= 0, got %d", low)
	}
	if high < low {
		return RangeBounds{}, fmt.Errorf("high must be >= low (%d), got %d", low, high)
	}
	return RangeBounds{Low: low, High: high}, nil
}

// KeyLeaseClaim is one Claim outcome. Token is set only when
// acquired; Release matches on it.
type KeyLeaseClaim struct {
	Verdict         KeyLeaseVerdict
	TopicId         int64
	ConsumerGroupId int64
	MessageKey      string
	Token           uuid.UUID
}

// Claim attempts to acquire the exclusive right to run a keyed message.
// For a compacted message, KeyLeaseAcquired also guarantees it was
// still its key's compaction head after the lease was won; an uncompacted
// message is never superseded.
// For ConcurrencyOrdered the claim is also refused (busy) while an earlier
// same-key message is unresolved for the group -- except one inside ownRange,
// which the caller orders itself; a compacted message keeps the compaction
// head as its gate whatever the policy says.
// Expiry does not stop a holder: the next claim on the key takes the lease
// over, and the two runs can overlap until the old one returns.
func (c *KeyLeaseController) Claim(ctx context.Context, topicId int64, groupId int64, key string, messageId int64, compacted bool, policy common.ConcurrencyPolicy, ownRange RangeBounds, duration time.Duration) (*KeyLeaseClaim, error) {
	if topicId <= 0 {
		return nil, fmt.Errorf("topicId must be > 0, got %d", topicId)
	}
	if groupId <= 0 {
		return nil, fmt.Errorf("groupId must be > 0, got %d", groupId)
	}
	if key == "" {
		return nil, errors.New("key is required")
	}
	if messageId <= 0 {
		return nil, fmt.Errorf("messageId must be > 0, got %d", messageId)
	}
	if err := policy.Validate(); err != nil {
		return nil, fmt.Errorf("policy: %w", err)
	}
	if ownRange.High < ownRange.Low {
		return nil, fmt.Errorf("ownRange.High must be >= ownRange.Low (%d), got %d", ownRange.Low, ownRange.High)
	}
	if duration <= 0 {
		return nil, fmt.Errorf("duration must be > 0, got %v", duration)
	}

	// minted once, before the datastore's retry loop -- claimSql's token match
	// lets a retry after an ambiguous commit re-take its own lease
	token := uuid.NewV7()

	data, err := c.datastore.Claim(ctx, topicId, groupId, key, messageId, compacted, policy, ownRange.Low, ownRange.High, duration, toTokenData(token))
	if err != nil || data == nil {
		return nil, err
	}
	return toKeyLeaseClaim(data), nil
}

// Release frees an acquired key.
// false -> no row matched the claim's Token: the lease expired, and the row
// was taken over or deleted by the janitor.
func (c *KeyLeaseController) Release(ctx context.Context, claim *KeyLeaseClaim) (bool, error) {
	if claim == nil {
		return false, errors.New("claim must not be nil")
	}
	if claim.TopicId <= 0 {
		return false, fmt.Errorf("claim.TopicId must be > 0, got %d", claim.TopicId)
	}
	if claim.Verdict != KeyLeaseAcquired {
		return false, fmt.Errorf("only an acquired key lease can be released, got %q", claim.Verdict)
	}

	return c.datastore.Release(ctx, toKeyLease(claim))
}
