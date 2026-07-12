package consumer

import (
	"sync"
	"sync/atomic"
	"time"
)

type ConsumerMetrics struct {
	AbandonedRoutines AbandonedRoutines
}

// abandonedKey identifies one callSafely invocation that was abandoned
type abandonedKey struct {
	MessageId int64
	Attempt   int
}

type AbandonedRoutines struct {
	mu sync.Mutex
	// TODO - data map can grow unbound, should eventually bound like ConcurrentBoundedRingBuffer but less likely to matter
	data             map[abandonedKey]time.Time // abandonedKey -> AbandonedAt.
	monotonicTotal   atomic.Uint32              // sure fucking hope we don't need Uint64 here
	reclaimLatencies *ConcurrentBoundedRingBuffer[time.Duration]
}

func NewConsumerMetrics() *ConsumerMetrics {
	return &ConsumerMetrics{
		AbandonedRoutines: AbandonedRoutines{
			data: make(map[abandonedKey]time.Time),
			// TODO - expose capacity size as consumer config option eventually
			reclaimLatencies: NewConcurrentBoundedRingBuffer[time.Duration](256),
		},
	}
}

func (a *AbandonedRoutines) Add(messageId int64, attempt int) {
	a.monotonicTotal.Add(1)

	key := abandonedKey{MessageId: messageId, Attempt: attempt}

	a.mu.Lock()
	defer a.mu.Unlock()
	a.data[key] = time.Now()
}

func (a *AbandonedRoutines) Remove(messageId int64, attempt int) {
	key := abandonedKey{MessageId: messageId, Attempt: attempt}

	a.mu.Lock()
	abandonedAt, ok := a.data[key]
	if ok {
		delete(a.data, key)
	}
	a.mu.Unlock()

	if !ok {
		return // not abandoned -- ordinary completion, nothing to do
	}
	a.reclaimLatencies.Add(time.Since(abandonedAt))
}

// TrackingTotal - amount of Routines that were ever abandoned, never decreases.
func (a *AbandonedRoutines) TrackingTotal() int {
	return int(a.monotonicTotal.Load())
}

// CurrentTotal - current amount of hanging abandoned routines.
// If an abandoned routine closes by itself this total will decrease.
func (a *AbandonedRoutines) CurrentTotal() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.data)
}

// ReclaimLatency - Average amount of time it takes for abandoned routines to reclaim itself ie close out (if it ever does)
func (a *AbandonedRoutines) ReclaimLatency() time.Duration {
	values := a.reclaimLatencies.Values()

	// 0 infinity guard
	if len(values) == 0 {
		return time.Duration(0)
	}

	var total time.Duration
	for _, reclaimLatency := range values {
		total += reclaimLatency
	}

	return total / time.Duration(len(values))
}

// TODO - move ConcurrentBoundedRingBuffer into its own file once file refactoring is underway / complete

// Why did I create this over complicated ConcurrentBoundedRingBuffer?
// to not allow metrics data to grow unbounded
// and it was fun
// And also fuck you
// thats why

// ConcurrentBoundedRingBuffer is safe for concurrent use
// Zero value is not usable -- always construct via NewConcurrentBoundedRingBuffer.
type ConcurrentBoundedRingBuffer[Data any] struct {
	mu          sync.Mutex
	boundedData []Data
	head        int
}

func NewConcurrentBoundedRingBuffer[Data any](capacity int) *ConcurrentBoundedRingBuffer[Data] {
	return &ConcurrentBoundedRingBuffer[Data]{
		boundedData: make([]Data, 0, capacity),
		head:        0,
	}
}

func (b *ConcurrentBoundedRingBuffer[Data]) Add(data Data) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if len(b.boundedData) < cap(b.boundedData) {
		b.boundedData = append(b.boundedData, data)
	} else { // reached capacity wrap around now following head
		b.boundedData[b.head] = data
		b.head = (b.head + 1) % cap(b.boundedData) // continue to move head along -> wrap at edge
	}
}

func (b *ConcurrentBoundedRingBuffer[Data]) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.boundedData)
}

func (b *ConcurrentBoundedRingBuffer[Data]) Values() []Data {
	b.mu.Lock()
	defer b.mu.Unlock()

	values := make([]Data, len(b.boundedData))
	copy(values, b.boundedData)
	return values
}
