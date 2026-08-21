package metrics

import "time"

// ConsumerGroupSnapshot is the live, DB-truth picture of one (group, topic),
// sectioned by the store each number reads -- answers "what's true right now"
// for state that multiple consumer processes share.
type ConsumerGroupSnapshot struct {
	ConsumerGroup string // whose picture this is

	Cursor            CursorSnapshot           // the group's cursor row against the message log
	Exceptions        ExceptionSnapshot        // the group's delivery rows counted by status
	OpenLeases        int64                    // the group's lease rows
	AbandonedRoutines AbandonedRoutineSnapshot // the group's abandoned/cleared events on __system.metrics
}

// CursorSnapshot is the group's read/commit position against the message log.
type CursorSnapshot struct {
	Head      int64 // highest message id ever appended -- the log frontier
	Claimed   int64 // cursor.claimed -- the read frontier
	Committed int64 // cursor.committed -- everything <= this is done/dead

	Backlog  int64 // Head - Committed
	Inflight int64 // Claimed - Committed -- claimed but not yet resolved
}

// ExceptionSnapshot is the group's delivery rows counted by status.
type ExceptionSnapshot struct {
	Ready    int64 // retryable, will be reclaimed
	Inflight int64 // currently leased out to a retry attempt
	Deferred int64 // waiting for their compaction key's key_lease to free
	Dead     int64 // DLQ size

	OldestUnresolvedAge time.Duration // age of the oldest ready/inflight/deferred row; 0 if none outstanding
}

// GroupLag is a group's drain progress -- the retire-relevant distillation
// of its snapshot.
type GroupLag struct {
	ConsumerGroup        string
	Committed            int64
	Head                 int64
	Lag                  int64 // Head - Committed, floored at 0
	UnresolvedExceptions int64 // delivery rows still 'ready', 'inflight', or 'deferred'
}

func (s *ConsumerGroupSnapshot) GroupLag() GroupLag {
	return GroupLag{
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
	Claimed    int64 // messages claimed
	Success    int64 // deliveries recorded 'success'
	Superseded int64 // deliveries recorded 'superseded'
	Ready      int64 // delivery rows written 'ready'
	Deferred   int64 // delivery rows written 'deferred'
	Dead       int64 // delivery rows written 'dead'

	Reclaimed   int64 // leases reclaimed from expired workers
	Quarantined int64 // ranges quarantined after max reclaims
	Abandoned   int64 // consumerFunc goroutines abandoned past the hard timeout
	LeaseLost   int64 // commits rejected because the lease was reclaimed
}
