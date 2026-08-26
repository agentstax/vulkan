package logging

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"time"
)

const suppressionWindow = time.Minute

// suppressHandler collapses repeats of one (level, message) Warn/Error
// line: the first occurrence passes, repeats inside suppressionWindow
// are dropped and counted, and the next emission of the same key after
// the window carries the count as suppressed_count. The state lives here
// and nowhere else -- pipeline rebuilds carry the node over, so all an
// instance's workers count into one window.
type suppressHandler struct {
	mutex sync.Mutex
	seen  map[suppressionKey]*suppressionState
}

// keys are static messages, so the map is bounded by the number of
// distinct Warn/Error lines the instance can emit -- no eviction needed
type suppressionKey struct {
	level   slog.Level
	message string
}

type suppressionState struct {
	windowStart     time.Time
	suppressedCount int
}

func newSuppressHandler() *suppressHandler {
	return &suppressHandler{seen: map[suppressionKey]*suppressionState{}}
}

func (s *suppressHandler) handle(ctx context.Context, record *record) *record {
	if record.level >= slog.LevelWarn {
		extra, emit := s.admit(record.level, record.message)
		if !emit {
			return nil
		}
		if extra != nil {
			record.args = slices.Concat(record.args, extra)
		}
	}
	return record
}

// admit reports whether this occurrence is emitted, with the attribute pair
// that carries a rolled window's count.
func (s *suppressHandler) admit(level slog.Level, message string) ([]any, bool) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	key := suppressionKey{level: level, message: message}
	state, ok := s.seen[key]
	now := time.Now()

	// first occurrence ever -- emit clean, open the window
	if !ok {
		s.seen[key] = &suppressionState{windowStart: now}
		return nil, true
	}

	// inside the window -- drop and count
	if now.Sub(state.windowStart) < suppressionWindow {
		state.suppressedCount++
		return nil, false
	}

	// window rolled -- emit, carry the count, reset
	suppressedCount := state.suppressedCount
	state.windowStart = now
	state.suppressedCount = 0
	if suppressedCount == 0 {
		return nil, true
	}
	return []any{"suppressed_count", suppressedCount}, true
}
