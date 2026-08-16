package metrics

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
)

// SampleNameReservedPrefix marks Vulkan's own samples -- user producers
// must not use it.
const SampleNameReservedPrefix = "vulkan."

// worker claim state, fleet-wide
const (
	SampleUnclaimedWorkers   = "vulkan.worker.state.unclaimed_workers"    // workers with no live instance and a nonzero target
	SampleOldestUnclaimedAge = "vulkan.worker.state.oldest_unclaimed_age" // largest now() - expires_at among unclaimed workers, in ms
	SampleFailingWorkers     = "vulkan.worker.state.failing_workers"      // workers with a live instance on a nonzero failure streak
)

// cron-job schedule health, fleet-wide
const (
	SampleOverdueJobs   = "vulkan.cron.state.overdue_jobs"   // unsuspended jobs due past the overdue threshold -- nothing is producing them
	SampleOldestDueAge  = "vulkan.cron.state.oldest_due_age" // largest now() - next_scheduled_time among unsuspended jobs, in ms
	SampleSuspendedJobs = "vulkan.cron.state.suspended_jobs" // jobs with suspended = true, excluded from the overdue count
)

// topic state -- attributes: topic, version
const (
	SampleTopicCompacted = "vulkan.topic.state.compacted" // 1 once the topic has ever seen a keyed publish, else 0
)

// consumer-group state -- attributes: group, topic, version
const (
	SampleCursorHead                   = "vulkan.consumer.cursor.head"                               // highest message id ever appended -- the log frontier
	SampleCursorClaimed                = "vulkan.consumer.cursor.claimed"                            // the group's read frontier
	SampleCursorCommitted              = "vulkan.consumer.cursor.committed"                          // everything at or below this id is done or dead
	SampleCursorBacklog                = "vulkan.consumer.cursor.backlog"                            // head - committed -- the waterline gap
	SampleCursorInflight               = "vulkan.consumer.cursor.inflight"                           // claimed - committed -- claimed but not yet resolved
	SampleReadyExceptions              = "vulkan.consumer.exceptions.ready"                          // delivery rows waiting to be retried
	SampleInflightExceptions           = "vulkan.consumer.exceptions.inflight"                       // delivery rows leased out to a retry attempt
	SampleDeferredExceptions           = "vulkan.consumer.exceptions.deferred"                       // delivery rows waiting for their compaction key's key_lease to free
	SampleDeadExceptions               = "vulkan.consumer.exceptions.dead"                           // dead-lettered delivery rows -- DLQ size
	SampleOldestUnresolvedAge          = "vulkan.consumer.exceptions.oldest_unresolved_age"          // age of the oldest ready/inflight/deferred row, in ms
	SampleOpenLeases                   = "vulkan.consumer.open_leases"                               // currently open leases for the (group, topic)
	SampleAbandonedOutstanding         = "vulkan.consumer.abandoned_routines.outstanding"            // abandoned events with no matching cleared event
	SampleAbandonedTotal               = "vulkan.consumer.abandoned_routines.total"                  // distinct abandoned events within the metrics topic's retention window
	SampleAbandonedSelfClearLatencyAvg = "vulkan.consumer.abandoned_routines.self_clear_latency_avg" // mean cleared - abandoned latency over matched pairs, in ms
)

type Kind string

const (
	KindGauge   Kind = "gauge"   // a point-in-time level, each sample replaces the last
	KindCounter Kind = "counter" // a running total, each sample carries the new total
)

func (k Kind) Validate() error {
	switch k {
	case KindGauge, KindCounter:
		return nil
	default:
		return fmt.Errorf("kind must be %q or %q, got %q", KindGauge, KindCounter, k)
	}
}

// Unit is a metric's UCUM code. A real unit ("ms", "s", "By") carries a
// dimension a reader may format (47000 ms -> 47s); a braced annotation
// ("{worker}", via UnitCount) is a dimensionless count whose text is a human
// label only. "" is no unit.
type Unit string

const UnitMilliseconds Unit = "ms"

// UnitCount is the UCUM annotation for a dimensionless count of noun.
// Ex: UnitCount("worker") -> "{worker}"
func UnitCount(noun string) Unit {
	return Unit("{" + noun + "}")
}

// Validate checks UCUM shape -- the unit set is open, so
// only whitespace and malformed annotations can be rejected.
func (u Unit) Validate() error {
	inAnnotation := false
	for _, character := range u {
		switch {
		case unicode.IsSpace(character):
			return fmt.Errorf("unit %q must not contain whitespace", u)
		case character == '{':
			if inAnnotation {
				return fmt.Errorf("unit %q nests an annotation", u)
			}
			inAnnotation = true
		case character == '}':
			if !inAnnotation {
				return fmt.Errorf("unit %q closes an unopened annotation", u)
			}
			inAnnotation = false
		}
	}
	if inAnnotation {
		return fmt.Errorf("unit %q leaves an annotation unclosed", u)
	}
	if strings.Contains(string(u), "{}") {
		return fmt.Errorf("unit %q has an empty annotation", u)
	}
	return nil
}

// Sample is one metric point on the __system.metrics topic. Names starting
// with "vulkan." are reserved for Vulkan's own samples.
type Sample struct {
	Name       string            `json:"name"`
	Kind       Kind              `json:"kind"`
	Value      float64           `json:"value"`
	Unit       Unit              `json:"unit"`
	Attributes map[string]string `json:"attributes"`
	At         time.Time         `json:"at"`
}

func NewSample(name string, kind Kind, value float64, unit Unit, attributes map[string]string, at time.Time) (*Sample, error) {
	if name == "" {
		return nil, errors.New("name is required")
	}
	if err := kind.Validate(); err != nil {
		return nil, err
	}
	if err := unit.Validate(); err != nil {
		return nil, err
	}
	if at.IsZero() {
		return nil, errors.New("at is required")
	}

	return &Sample{
		Name:       name,
		Kind:       kind,
		Value:      value,
		Unit:       unit,
		Attributes: attributes,
		At:         at,
	}, nil
}

// SampleKey is the compaction key a Sample is produced under. Attribute keys
// are sorted, so equal attribute sets always yield one key -- map iteration
// order must never reach it.
//
// Ex: ("lag", {"group": "billing", "topic": "orders"}) -> "lag|group=billing,topic=orders"
// Ex: ("lag", nil) -> "lag"
func SampleKey(name string, attributes map[string]string) string {
	if len(attributes) == 0 {
		return name
	}

	keys := make([]string, 0, len(attributes))
	for key := range attributes {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var builder strings.Builder
	builder.WriteString(name)
	for i, key := range keys {
		if i == 0 {
			builder.WriteString("|")
		} else {
			builder.WriteString(",")
		}
		builder.WriteString(key)
		builder.WriteString("=")
		builder.WriteString(attributes[key])
	}
	return builder.String()
}
