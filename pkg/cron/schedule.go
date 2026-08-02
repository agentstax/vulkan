package cron

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/agentstax/vulkan/pkg/cron/internal/robfig"
)

const (
	minGapFirings = 1000
	minGapHorizon = 400 * 24 * time.Hour
)

type Schedule struct {
	// the parsed form can't turn back into text -- this is what cron_job stores
	expr   string
	sched  robfig.Schedule
	minGap time.Duration
}

// ParseSchedule parses a 5-field cron spec or a descriptor, UTC unless the
// expr is TZ= prefixed.
//   - spec: "30 4 * * 1" -- 04:30 every Monday
//   - descriptor: "@hourly", "@every 90m"
//   - zoned: "TZ=America/New_York 0 9 * * *" -- 09:00 New York time
func ParseSchedule(expr string) (*Schedule, error) {
	sched, err := robfig.ParseStandard(expr)
	if err != nil {
		return nil, err
	}
	gap, err := minGap(sched)
	if err != nil {
		return nil, fmt.Errorf("schedule %q: %w", expr, err)
	}
	if gap < time.Minute {
		return nil, fmt.Errorf("schedule %q fires every %v -- more often than the 1m scheduler resolution", expr, gap)
	}
	return &Schedule{expr: expr, sched: sched, minGap: gap}, nil
}

// Next is the next firing strictly after t; zero = never fires again.
func (s *Schedule) Next(t time.Time) time.Time {
	return s.sched.Next(t)
}

func (s *Schedule) String() string {
	return s.expr
}

// MinGap is the smallest firing gap over the next 1000 firings or 400 days.
// Fewer than two firings in that window (Feb-29-style) -> math.MaxInt64.
func (s *Schedule) MinGap() time.Duration {
	return s.minGap
}

func minGap(sched robfig.Schedule) (time.Duration, error) {
	start := time.Now().UTC()
	prev := sched.Next(start)
	if prev.IsZero() {
		return 0, errors.New("never fires")
	}

	horizon := start.Add(minGapHorizon)
	min := time.Duration(math.MaxInt64)
	for range minGapFirings - 1 {
		n := sched.Next(prev)
		if n.IsZero() || n.After(horizon) {
			break
		}
		if gap := n.Sub(prev); gap < min {
			min = gap
		}
		prev = n
	}
	return min, nil
}
