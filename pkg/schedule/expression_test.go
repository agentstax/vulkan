package schedule

import (
	"math"
	"testing"
	"time"
)

func TestParseExpressionDefaultsToUTC(t *testing.T) {
	sched, err := ParseExpression("0 9 * * *")
	if err != nil {
		t.Fatal(err)
	}

	// from 08:00 UTC expressed in a non-UTC zone: a UTC schedule comes due at
	// 09:00 UTC; a zone-following schedule would fire at 09:00+05:30
	start := time.Date(2026, 3, 2, 13, 30, 0, 0, time.FixedZone("IST", 5*3600+1800))
	next := sched.Next(start)
	expected := time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Errorf("expected %v, got %v", expected, next)
	}
}

func TestMinRate(t *testing.T) {
	unbounded := time.Duration(math.MaxInt64)
	cases := []struct {
		expr     string
		expected time.Duration
	}{
		{"* * * * *", time.Minute},
		{"*/5 * * * *", 5 * time.Minute},
		{"@hourly", time.Hour},
		{"0 0 * * *", 24 * time.Hour}, // UTC daily -- no DST, constant rate
		{"@every 90s", 90 * time.Second},

		// 23h fall-back day is the min rate across a year of NY daily scheduled times
		{"TZ=America/New_York 0 0 * * *", 23 * time.Hour},

		// recurs once every 4 years -- one scheduled time inside the 400d horizon at most
		{"0 0 29 2 *", unbounded},
	}
	for _, c := range cases {
		sched, err := ParseExpression(c.expr)
		if err != nil {
			t.Errorf("%q: %v", c.expr, err)
			continue
		}
		if sched.MinRate() != c.expected {
			t.Errorf("%q: expected min rate %v, got %v", c.expr, c.expected, sched.MinRate())
		}
	}
}

func TestParseExpressionRejectsUnschedulable(t *testing.T) {
	cases := []struct {
		name string
		expr string
	}{
		{name: "malformed", expr: "not a cron expr"},
		{name: "faster than scheduler resolution", expr: "@every 30s"},
		{name: "never comes due", expr: "0 0 30 2 *"}, // Feb 30 does not exist
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := ParseExpression(c.expr); err == nil {
				t.Errorf("%q: expected an error", c.expr)
			}
		})
	}
}
