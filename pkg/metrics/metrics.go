package metrics

import (
	"time"
)

// ConsumerGroupSnapshot is the live, DB-truth picture of one (group, topic),
// sectioned by the store each number reads -- answers "what's true right now"
// for state that multiple consumer processes share.
type ConsumerGroupSnapshot struct {
	ConsumerGroup string `json:"group"` // whose picture this is

	Cursor            CursorSnapshot           `json:"cursor"`             // the group's cursor row against the message log
	Exceptions        ExceptionSnapshot        `json:"exceptions"`         // the group's delivery rows counted by status
	OpenLeases        int64                    `json:"open_leases"`        // the group's lease rows
	AbandonedRoutines AbandonedRoutineSnapshot `json:"abandoned_routines"` // the group's abandoned/cleared events on __system.metrics
}

// CursorSnapshot is the group's read/commit position against the message log.
type CursorSnapshot struct {
	Head      int64 `json:"head"`      // highest message id ever appended -- the log frontier
	Claimed   int64 `json:"claimed"`   // cursor.claimed -- the read frontier
	Committed int64 `json:"committed"` // cursor.committed -- everything <= this is done/dead

	Backlog  int64 `json:"backlog"`  // Head - Committed
	Inflight int64 `json:"inflight"` // Claimed - Committed -- claimed but not yet resolved
}

// ExceptionSnapshot is the group's delivery rows counted by status.
type ExceptionSnapshot struct {
	Ready    int64 `json:"ready"`    // retryable, will be reclaimed
	Inflight int64 `json:"inflight"` // currently leased out to a retry attempt
	Deferred int64 `json:"deferred"` // waiting for their message key's lease to free
	Dead     int64 `json:"dead"`     // DLQ size

	OldestUnresolvedAge time.Duration `json:"oldest_unresolved_age"` // age of the oldest ready/inflight/deferred row; 0 if none outstanding
}

// ConsumerGroupLag is a group's drain progress -- the retire-relevant distillation
// of its snapshot.
type ConsumerGroupLag struct {
	ConsumerGroup        string `json:"group"`
	Committed            int64  `json:"committed"`
	Head                 int64  `json:"head"`
	Lag                  int64  `json:"lag"`                   // Head - Committed, floored at 0
	UnresolvedExceptions int64  `json:"unresolved_exceptions"` // delivery rows still 'ready', 'inflight', or 'deferred'
}

// Lag returns the group's drain progress.
func (s *ConsumerGroupSnapshot) Lag() ConsumerGroupLag {
	return ConsumerGroupLag{
		ConsumerGroup:        s.ConsumerGroup,
		Committed:            s.Cursor.Committed,
		Head:                 s.Cursor.Head,
		Lag:                  max(s.Cursor.Backlog, 0),
		UnresolvedExceptions: s.Exceptions.Ready + s.Exceptions.Inflight + s.Exceptions.Deferred,
	}
}

// SessionCounters is one consumer instance's lifetime totals, counted in
// memory as the work happens -- the instance's own contribution, where
// ConsumerGroupSnapshot is the fleet-wide DB truth.
type SessionCounters struct {
	Claimed    int64 `json:"claimed_count"`    // messages claimed
	Success    int64 `json:"success_count"`    // deliveries recorded 'success'
	Superseded int64 `json:"superseded_count"` // deliveries recorded 'superseded'
	Ready      int64 `json:"ready_count"`      // delivery rows written 'ready'
	Deferred   int64 `json:"deferred_count"`   // delivery rows written 'deferred'
	Dead       int64 `json:"dead_count"`       // delivery rows written 'dead'

	Reclaimed   int64 `json:"reclaimed_count"`   // leases reclaimed from expired workers
	Quarantined int64 `json:"quarantined_count"` // ranges quarantined after max reclaims
	Abandoned   int64 `json:"abandoned_count"`   // consumerFunc goroutines abandoned past the hard timeout
	LeaseLost   int64 `json:"lease_lost_count"`  // commits rejected because the lease was reclaimed
}
