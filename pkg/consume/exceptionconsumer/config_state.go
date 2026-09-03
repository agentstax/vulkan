package exceptionconsumer

import (
	"errors"
	"sync"
)

// configState is the group config this instance runs on: the resolved copy
// every operation reads, and the declaration that copy was resolved from.
// The refresh loop replaces both together, so a claim never reads the row.
type configState struct {
	// guards both fields -- the declaration decides whether cfg swaps, so the
	// two are never read or written apart.
	mu       sync.RWMutex
	cfg      *ExceptionConsumerConfig
	declared *ExceptionConsumerMetadata
}

func newConfigState(cfg *ExceptionConsumerConfig, declared *ExceptionConsumerMetadata) (*configState, error) {
	if cfg == nil {
		return nil, errors.New("cfg must not be nil")
	}
	if declared == nil {
		return nil, errors.New("declared must not be nil")
	}
	return &configState{cfg: cfg, declared: declared}, nil
}

// current is the copy one operation runs on -- a claim, a tick, a drain.
// Taking it once means a mid-operation replace cannot mix two configs.
func (s *configState) current() *ExceptionConsumerConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

// replace returns false when the declaration is unchanged and nothing swapped.
func (s *configState) replace(declared *ExceptionConsumerMetadata) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if declared.Equal(s.declared) {
		return false
	}

	// only session fields carry over -- withMetadata replaces every stored one
	s.cfg = s.cfg.withMetadata(declared)
	s.declared = declared
	return true
}
