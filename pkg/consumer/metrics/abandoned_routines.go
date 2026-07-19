package metrics

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// abandonedKey identifies one callSafely invocation that was abandoned
type abandonedKey struct {
	MessageId int64
	Attempt   int
}

// AbandonedRoutinesSnapshot is the locally-readable picture of AbandonedRoutines
// state -- everything the otel instruments accept but can't hand back, mirrored
// here so a caller can read it with no exporter/backend attached.
type AbandonedRoutinesSnapshot struct {
	Total               int
	Outstanding         int
	SelfClearLatencyAvg time.Duration
}

type AbandonedRoutines struct {
	// otel instruments
	total            metric.Int64Counter
	outstanding      metric.Int64UpDownCounter
	selfClearLatency metric.Int64Histogram
	// group/topic identity, precomputed once so every Add/Remove call reuses
	// the same option instead of rebuilding an attribute slice per event.
	attrs metric.MeasurementOption

	// local mirror -- Snapshot reads these, never the instruments above
	mu sync.Mutex
	// deliberately unbounded -- an entry lives exactly as long as its abandoned
	// goroutine which dwarf these entries in cost.
	data               map[abandonedKey]time.Time // abandonedKey -> AbandonedAt.
	monotonicTotal     atomic.Uint32              // sure fucking hope we don't need Uint64 here
	selfClearLatencies *ConcurrentBoundedRingBuffer[time.Duration]
}

func NewAbandonedRoutines(meter metric.Meter, group string, topicName string) (*AbandonedRoutines, error) {
	total, err := meter.Int64Counter(
		"vulkan.consumer.abandoned_routines.total",
		metric.WithDescription("Total consumerFunc invocations abandoned after exceeding WorkTimeout + WorkTimeoutGrace. Monotonic, never decreases."),
		metric.WithUnit("{routine}"),
	)
	if err != nil {
		return nil, err
	}

	outstanding, err := meter.Int64UpDownCounter(
		"vulkan.consumer.abandoned_routines.outstanding",
		metric.WithDescription("Abandoned goroutines currently still running in the background, not yet self-cleared."),
		metric.WithUnit("{routine}"),
	)
	if err != nil {
		return nil, err
	}

	selfClearLatency, err := meter.Int64Histogram(
		"vulkan.consumer.abandoned_routines.self_clear_latency",
		metric.WithDescription("Time between a routine being abandoned and it self-clearing, for the ones that do eventually return."),
		metric.WithUnit("ms"),
		// default buckets (0-10s) are shaped for request latency, not this --
		// the clock only starts once a routine already blew past WorkTimeout+
		// Grace, so "late" here means anywhere from milliseconds to minutes.
		// Advisory only -- a caller's own SDK View can still override it.
		metric.WithExplicitBucketBoundaries(10, 50, 100, 500, 1000, 2500, 5000, 10000, 30000, 60000, 300000),
	)
	if err != nil {
		return nil, err
	}

	// WithAttributeSet over WithAttributes -- the latter defensively copies
	// the slice on every call (for concurrent callers), which is wasted work
	// here since this set is built once, non-concurrently, and reused.
	attrs := metric.WithAttributeSet(attribute.NewSet(
		attribute.String("messaging.consumer.group.name", group),
		attribute.String("messaging.destination.name", topicName),
	))

	return &AbandonedRoutines{
		// otel instruments
		total:            total,
		outstanding:      outstanding,
		selfClearLatency: selfClearLatency,
		attrs:            attrs,

		// local mirror
		data: make(map[abandonedKey]time.Time),
		// TODO - expose capacity size as consumer config option eventually
		selfClearLatencies: NewConcurrentBoundedRingBuffer[time.Duration](256),
	}, nil
}

func (a *AbandonedRoutines) Add(ctx context.Context, messageId int64, attempt int) {
	a.total.Add(ctx, 1, a.attrs)
	a.outstanding.Add(ctx, 1, a.attrs)
	a.monotonicTotal.Add(1) // otel counters are write-only -- this is what Snapshot reads back

	key := abandonedKey{MessageId: messageId, Attempt: attempt}

	a.mu.Lock()
	defer a.mu.Unlock()
	a.data[key] = time.Now()
}

// Remove clears an abandoned routine that finally returned (self-cleared).
// The not-found branch is defensive -- callers pair every Remove with a prior Add.
func (a *AbandonedRoutines) Remove(ctx context.Context, messageId int64, attempt int) {
	key := abandonedKey{MessageId: messageId, Attempt: attempt}

	a.mu.Lock()
	abandonedAt, ok := a.data[key]
	if ok {
		delete(a.data, key)
	}
	a.mu.Unlock()

	if !ok {
		return // never Added -- decrementing outstanding here would corrupt it
	}

	a.outstanding.Add(ctx, -1, a.attrs)
	a.selfClearLatency.Record(ctx, time.Since(abandonedAt).Abs().Milliseconds(), a.attrs)
	a.selfClearLatencies.Add(time.Since(abandonedAt))
}

// Snapshot is the current picture of AbandonedRoutines state, read entirely
// from the local mirror -- works with no otel exporter/backend attached.
func (a *AbandonedRoutines) Snapshot() AbandonedRoutinesSnapshot {
	a.mu.Lock()
	outstanding := len(a.data)
	a.mu.Unlock()

	return AbandonedRoutinesSnapshot{
		Total:               int(a.monotonicTotal.Load()),
		Outstanding:         outstanding,
		SelfClearLatencyAvg: a.selfClearLatencyAvg(),
	}
}

// selfClearLatencyAvg - average time it takes an abandoned routine to self-
// clear, i.e. close out on its own (if it ever does).
func (a *AbandonedRoutines) selfClearLatencyAvg() time.Duration {
	values := a.selfClearLatencies.Values()

	// 0 infinity guard
	if len(values) == 0 {
		return time.Duration(0)
	}

	var total time.Duration
	for _, latency := range values {
		total += latency
	}

	return total / time.Duration(len(values))
}
