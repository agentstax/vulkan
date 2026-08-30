package metrics

import (
	"errors"
	"fmt"
	"github.com/agentstax/vulkan/pkg/topic"
	"sort"
	"strings"
	"time"
	"unicode"
)

// MetricNameReservedPrefix marks Vulkan's own metrics -- user producers
// must not use it.
const MetricNameReservedPrefix = "vulkan."

// worker claim state, fleet-wide
const (
	MetricUnclaimedWorkers   = "vulkan.worker.state.unclaimed_workers"    // workers with no live instance and a nonzero target
	MetricOldestUnclaimedAge = "vulkan.worker.state.oldest_unclaimed_age" // largest now() - expires_at among unclaimed workers, in ms
	MetricFailingWorkers     = "vulkan.worker.state.failing_workers"      // workers with a live instance on a nonzero failure streak
)

// cron-job schedule health, fleet-wide
const (
	MetricOverdueJobs   = "vulkan.cron.state.overdue_jobs"   // unsuspended jobs due past the overdue threshold -- nothing is producing them
	MetricOldestDueAge  = "vulkan.cron.state.oldest_due_age" // largest now() - next_scheduled_at among unsuspended jobs, in ms
	MetricSuspendedJobs = "vulkan.cron.state.suspended_jobs" // jobs with suspended = true, excluded from the overdue count
)

// alert state, fleet-wide
const (
	MetricActiveAlerts   = "vulkan.alert.state.active_alerts"   // heads with status 'active'
	MetricResolvedAlerts = "vulkan.alert.state.resolved_alerts" // heads with status 'resolved', until retention sweeps them
)

// alert check runs, one set of below per run
const (
	MetricCheckTopicsEvaluated = "vulkan.alert.check.topics_evaluated" // topics the run checked
	MetricCheckTopicsFailed    = "vulkan.alert.check.topics_failed"    // topics whose evaluate or publish errored
	MetricCheckPublishedAlerts = "vulkan.alert.check.published_alerts" // Record calls that published a new active alert
	MetricCheckResolvedAlerts  = "vulkan.alert.check.resolved_alerts"  // Record calls that published the head resolved
)

// topic state -- attributes: topic, version
const (
	MetricTopicCompacted = "vulkan.topic.state.compacted" // 1 once the topic has ever seen a keyed publish, else 0
)

// consumer-group state -- attributes: group, topic, version
const (
	MetricCursorHead                   = "vulkan.consumer.cursor.head"                               // highest message id ever appended -- the log frontier
	MetricCursorClaimed                = "vulkan.consumer.cursor.claimed"                            // the group's read frontier
	MetricCursorCommitted              = "vulkan.consumer.cursor.committed"                          // everything at or below this id is done or dead
	MetricCursorBacklog                = "vulkan.consumer.cursor.backlog"                            // head - committed
	MetricCursorInflight               = "vulkan.consumer.cursor.inflight"                           // claimed - committed -- claimed but not yet resolved
	MetricReadyExceptions              = "vulkan.consumer.exceptions.ready"                          // delivery rows waiting to be retried
	MetricInflightExceptions           = "vulkan.consumer.exceptions.inflight"                       // delivery rows leased out to a retry attempt
	MetricDeferredExceptions           = "vulkan.consumer.exceptions.deferred"                       // delivery rows waiting for their message key's lease to free
	MetricDeadExceptions               = "vulkan.consumer.exceptions.dead"                           // dead-lettered delivery rows -- DLQ size
	MetricOldestUnresolvedAge          = "vulkan.consumer.exceptions.oldest_unresolved_age"          // age of the oldest ready/inflight/deferred row, in ms
	MetricOpenLeases                   = "vulkan.consumer.open_leases"                               // currently open leases for the (group, topic)
	MetricAbandonedOutstanding         = "vulkan.consumer.abandoned_routines.outstanding"            // abandoned events with no matching cleared event
	MetricAbandonedTotal               = "vulkan.consumer.abandoned_routines.total"                  // distinct abandoned events within the metrics topic's retention window
	MetricAbandonedSelfClearLatencyAvg = "vulkan.consumer.abandoned_routines.self_clear_latency_avg" // mean cleared - abandoned latency over matched pairs, in ms
)

type Kind string

const (
	KindGauge   Kind = "gauge"   // a point-in-time level, each measurement replaces the last
	KindCounter Kind = "counter" // a running total, each measurement carries the new total
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

// Measurement is one value of one metric at one time, on the __system.metrics
// topic. Names starting with "vulkan." are reserved for Vulkan's own metrics.
type Measurement struct {
	Name       string            `json:"name"`
	Kind       Kind              `json:"kind"`
	Value      float64           `json:"value"`
	Unit       Unit              `json:"unit"`
	Attributes map[string]string `json:"attributes"`
	At         time.Time         `json:"at"`
}

func (Measurement) SchemaVersion() topic.SchemaVersion { return 1 }

func NewMeasurement(name string, kind Kind, value float64, unit Unit, attributes map[string]string, at time.Time) (*Measurement, error) {
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

	return &Measurement{
		Name:       name,
		Kind:       kind,
		Value:      value,
		Unit:       unit,
		Attributes: attributes,
		At:         at,
	}, nil
}

// MeasurementKey is the message key a Measurement is produced under. Attribute keys
// are sorted, so equal attribute sets always yield one key -- map iteration
// order must never reach it.
//
// Ex: ("lag", {"group": "billing", "topic": "orders"}) -> "lag|group=billing,topic=orders"
// Ex: ("lag", nil) -> "lag"
func MeasurementKey(name string, attributes map[string]string) string {
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
