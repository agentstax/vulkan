package metrics

import (
	"time"

	"github.com/agentstax/vulkan/pkg/common/diagnostic"
)

// consumer session flows -- per-instance monotonic totals, one series per
// session. Flows = "what this instance did".
var (
	MetricSessionClaimed     = diagnostic.NewDiagnosticMetric("VK0042", "vulkan.consumer.session.claimed", string(MetricKindCounter), string(MetricUnitCount("message")), "messages this instance claimed -- cursor ranges and exception retries together")
	MetricSessionSuccess     = diagnostic.NewDiagnosticMetric("VK0043", "vulkan.consumer.session.success", string(MetricKindCounter), string(MetricUnitCount("message")), "consumerFunc runs that completed cleanly, counted at resolution")
	MetricSessionSuperseded  = diagnostic.NewDiagnosticMetric("VK0044", "vulkan.consumer.session.superseded", string(MetricKindCounter), string(MetricUnitCount("message")), "messages resolved without running -- a newer version of their compacted message key had already arrived")
	MetricSessionReady       = diagnostic.NewDiagnosticMetric("VK0045", "vulkan.consumer.session.ready", string(MetricKindCounter), string(MetricUnitCount("message")), "delivery rows written 'ready' -- each will be retried once its backoff passes")
	MetricSessionDeferred    = diagnostic.NewDiagnosticMetric("VK0046", "vulkan.consumer.session.deferred", string(MetricKindCounter), string(MetricUnitCount("message")), "delivery rows written 'deferred' -- another delivery held their message key")
	MetricSessionDead        = diagnostic.NewDiagnosticMetric("VK0047", "vulkan.consumer.session.dead", string(MetricKindCounter), string(MetricUnitCount("message")), "delivery rows written 'dead' -- exhausted retries, unrecoverable payloads, and the kill backstop")
	MetricSessionReclaimed   = diagnostic.NewDiagnosticMetric("VK0048", "vulkan.consumer.session.reclaimed", string(MetricKindCounter), string(MetricUnitCount("lease")), "leases taken over from expired workers -- another instance died or stalled mid-range")
	MetricSessionQuarantined = diagnostic.NewDiagnosticMetric("VK0049", "vulkan.consumer.session.quarantined", string(MetricKindCounter), string(MetricUnitCount("range")), "ranges past the reclaim cap, written out as independent 'ready' exceptions")
	MetricSessionAbandoned   = diagnostic.NewDiagnosticMetric("VK0050", "vulkan.consumer.session.abandoned", string(MetricKindCounter), string(MetricUnitCount("routine")), "consumerFunc goroutines written off past the hard timeout")
	MetricSessionLeaseLost   = diagnostic.NewDiagnosticMetric("VK0051", "vulkan.consumer.session.lease_lost", string(MetricKindCounter), string(MetricUnitCount("commit")), "commits rejected because another worker reclaimed the lease first -- that work was redone elsewhere")
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

// GroupLag is a group's drain progress -- the retire-relevant distillation
// of its snapshot.
type GroupLag struct {
	ConsumerGroup        string `json:"group"`
	Committed            int64  `json:"committed"`
	Head                 int64  `json:"head"`
	Lag                  int64  `json:"lag"`                   // Head - Committed, floored at 0
	UnresolvedExceptions int64  `json:"unresolved_exceptions"` // delivery rows still 'ready', 'inflight', or 'deferred'
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
