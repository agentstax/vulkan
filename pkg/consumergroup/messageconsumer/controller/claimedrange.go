package controller

import (
	"time"

	"github.com/google/uuid"
)

// a leased window of work -- the messages to process plus the lease that guards
// them. the worker frees the lease (Commit) once the whole range is done; the
// lazy roller then advances committed past it.
type ClaimedRange struct {
	Lease    RangeLease
	Messages []Message

	// Quarantined -> the reclaimable range hit max reclaims and was written
	// out as 'ready' exceptions instead; Messages is empty and the lease is
	// already freed. Nothing to dispatch or commit.
	Quarantined bool
}

// RangeLease guards a claimed (Low, High] window. Token is what every write
// against the range matches on -- a reclaim rotates it, so a stale worker's
// commit matches nothing.
type RangeLease struct {
	Token           uuid.UUID
	ConsumerGroupId int64
	Low             int64
	High            int64
	ExpiresAt       time.Time
	Reclaims        int
}
