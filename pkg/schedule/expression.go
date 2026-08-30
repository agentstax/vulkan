package schedule

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/agentstax/vulkan/pkg/schedule/internal/robfig"
)

const (
	minRateScheduledTimes = 1000
	minRateHorizon        = 400 * 24 * time.Hour
)

type Expression struct {
	// the parsed form can't turn back into text -- this is what schedule stores
	expr     string
	schedule robfig.Schedule
	minRate  time.Duration
}

// ParseExpression parses a 5-field cron spec or a descriptor, UTC unless the
// expr is TZ= prefixed.
//   - spec: "30 4 * * 1" -- 04:30 every Monday
//   - descriptor: "@hourly", "@every 90m"
//   - zoned: "TZ=America/New_York 0 9 * * *" -- 09:00 New York time
func ParseExpression(expr string) (*Expression, error) {
	schedule, err := robfig.ParseStandard(expr)
	if err != nil {
		return nil, err
	}
	rate, err := minRate(schedule)
	if err != nil {
		return nil, fmt.Errorf("expression %q: %w", expr, err)
	}
	if rate < time.Minute {
		return nil, fmt.Errorf("expression %q recurs every %v -- more often than the 1m schedule producer resolution", expr, rate)
	}
	return &Expression{expr: expr, schedule: schedule, minRate: rate}, nil
}

// Next is the next scheduled time strictly after the given time; zero = none remains.
func (s *Expression) Next(after time.Time) time.Time {
	return s.schedule.Next(after)
}

func (s *Expression) String() string {
	return s.expr
}

// MinRate is the shortest gap between consecutive scheduled times over the
// next 1000 scheduled times or 400 days.
// Fewer than two scheduled times in that window (Feb-29-style) -> math.MaxInt64.
func (s *Expression) MinRate() time.Duration {
	return s.minRate
}

// ***************
// *** HELPERS ***
// ***************

func minRate(schedule robfig.Schedule) (time.Duration, error) {
	start := time.Now().UTC()
	previous := schedule.Next(start)
	if previous.IsZero() {
		return 0, errors.New("no upcoming scheduled times")
	}

	horizon := start.Add(minRateHorizon)
	minimum := time.Duration(math.MaxInt64)
	for range minRateScheduledTimes - 1 {
		next := schedule.Next(previous)
		if next.IsZero() || next.After(horizon) {
			break
		}
		if rate := next.Sub(previous); rate < minimum {
			minimum = rate
		}
		previous = next
	}
	return minimum, nil
}
