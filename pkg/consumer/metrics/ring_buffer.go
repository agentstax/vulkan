package metrics

import "sync"

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
