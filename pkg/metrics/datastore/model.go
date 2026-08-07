package datastore

import "time"

// ConsumerGroupSnapshot is the live, DB-truth picture of one (group, topic)'s
// queue -- answers "what's true right now" for state that multiple consumer
// processes share (cursor/delivery/lease), which no in-process counter can.
type ConsumerGroupSnapshot struct {
	ConsumerGroup string // whose picture this is

	Head      int64 // highest message id ever appended -- the log frontier
	Claimed   int64 // cursor.claimed -- the read frontier
	Committed int64 // cursor.committed -- everything <= this is done/dead

	Backlog  int64 // Head - Committed -- the waterline gap
	Inflight int64 // Claimed - Committed -- claimed but not yet resolved

	ReadyExceptions    int64 // retryable, will be reclaimed
	InflightExceptions int64 // currently leased out to a retry attempt
	DeferredExceptions int64 // waiting for their compaction key's key_lease to free
	DeadExceptions     int64 // DLQ size

	OldestUnackedAge time.Duration // age of the oldest ready/inflight/deferred exception; 0 if none outstanding

	OpenLeases int64
}

// GroupLag is a group's drain progress -- the retire-relevant distillation
// of its snapshot.
type GroupLag struct {
	ConsumerGroup    string
	Committed        int64
	Head             int64
	Lag              int64 // Head - Committed, floored at 0
	ParkedExceptions int64 // delivery rows still 'ready', 'inflight', or 'deferred'
}

// DutySnapshot is one maintenance row's health.
type DutySnapshot struct {
	Duty          string
	TopicName     string
	ConsumerGroup string
	Rate          time.Duration
	GateAge       time.Duration // now() - can_run_after: negative while claimed into the future, positive once eligible and unclaimed
	Overdue       bool          // GateAge > overdueFactor * Rate -- nobody is maintaining this duty (or its owner is stuck)
	Attempts      int
}

// EventSnapshot is derived from the __system.metrics event stream for one
// (topic, group) -- no in-process counter is kept anywhere, every number
// here comes from pairing abandoned/cleared events already on the topic.
type EventSnapshot struct {
	Outstanding         int64         // abandoned events with no matching cleared
	Total               int64         // distinct abandoned keys currently in the window
	SelfClearLatencyAvg time.Duration // mean(cleared.At - abandoned.At) over matched pairs; 0 if no pair has cleared yet
}
