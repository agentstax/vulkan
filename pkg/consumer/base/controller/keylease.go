package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// KeyLeaseVerdict classifies a ClaimKeyLease attempt.
type KeyLeaseVerdict string

const (
	KeyLeaseAcquired   KeyLeaseVerdict = "acquired"   // the caller holds the key until release or expiry
	KeyLeaseBusy       KeyLeaseVerdict = "busy"       // another delivery holds the key
	KeyLeaseSuperseded KeyLeaseVerdict = "superseded" // the message is no longer its key's compaction head -- never run it
)

// KeyLeaseClaim is one ClaimKeyLease outcome. Token is set only when
// acquired; ReleaseKeyLease matches on it.
type KeyLeaseClaim struct {
	Verdict         KeyLeaseVerdict
	ConsumerGroupId int64
	CompactionKey   string
	Token           uuid.UUID
}

// ClaimKeyLease attempts to acquire the exclusive right to run a keyed
// message. KeyLeaseAcquired guarantees the message was still its key's
// compaction head after the lease was won.
// Expiry does not stop a holder: the next claim on the key takes the lease
// over, and the two runs can overlap until the old one returns.
func (c *KeyLeaseController) ClaimKeyLease(ctx context.Context, topicId int64, groupId int64, key string, messageId int64, duration time.Duration) (*KeyLeaseClaim, error) {
	if topicId <= 0 {
		return nil, errors.New("topicId must be > 0")
	}
	if groupId <= 0 {
		return nil, errors.New("groupId must be > 0")
	}
	if key == "" {
		return nil, errors.New("key is required")
	}
	if messageId <= 0 {
		return nil, fmt.Errorf("messageId must be > 0, got %d", messageId)
	}
	if duration <= 0 {
		return nil, fmt.Errorf("duration must be > 0, got %v", duration)
	}

	data, err := c.datastore.ClaimKeyLease(ctx, topicId, groupId, key, messageId, duration)
	if err != nil || data == nil {
		return nil, err
	}
	return toKeyLeaseClaim(data), nil
}

// ReleaseKeyLease frees an acquired key.
// false -> no row matched the claim's Token: the lease expired, and the row
// was taken over or deleted by the janitor.
func (c *KeyLeaseController) ReleaseKeyLease(ctx context.Context, claim *KeyLeaseClaim) (bool, error) {
	if claim == nil {
		return false, errors.New("claim must not be nil")
	}
	if claim.Verdict != KeyLeaseAcquired {
		return false, fmt.Errorf("only an acquired key lease can be released, got %q", claim.Verdict)
	}

	return c.datastore.ReleaseKeyLease(ctx, toKeyLeaseData(claim))
}
