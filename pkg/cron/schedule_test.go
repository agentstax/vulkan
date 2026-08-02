package cron

import (
	"math"
	"testing"
	"time"
)

func TestParseScheduleDefaultsToUTC(t *testing.T) {
	sched, err := ParseSchedule("0 9 * * *")
	if err != nil {
		t.Fatal(err)
	}
	// from 08:00 UTC expressed in a non-UTC zone: a UTC schedule fires at
	// 09:00 UTC; a zone-following schedule would fire at 09:00+05:30
	start := time.Date(2026, 3, 2, 13, 30, 0, 0, time.FixedZone("IST", 5*3600+1800))
	next := sched.Next(start)
	expected := time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Errorf("expected %v, got %v", expected, next)
	}
}

func TestMinGap(t *testing.T) {
	unbounded := time.Duration(math.MaxInt64)
	cases := []struct {
		expr     string
		expected time.Duration
	}{
		{"* * * * *", time.Minute},
		{"*/5 * * * *", 5 * time.Minute},
		{"@hourly", time.Hour},
		{"0 0 * * *", 24 * time.Hour}, // UTC daily -- no DST, constant gap
		{"@every 90s", 90 * time.Second},
		// 23h fall-back gap is the min across a year of NY daily firings
		{"TZ=America/New_York 0 0 * * *", 23 * time.Hour},
		// fires once every 4 years -- one firing inside the 400d horizon at most
		{"0 0 29 2 *", unbounded},
	}
	for _, c := range cases {
		sched, err := ParseSchedule(c.expr)
		if err != nil {
			t.Errorf("%q: %v", c.expr, err)
			continue
		}
		if sched.MinGap() != c.expected {
			t.Errorf("%q: expected min gap %v, got %v", c.expr, c.expected, sched.MinGap())
		}
	}
}

func TestParseScheduleRejectsUnschedulable(t *testing.T) {
	cases := []struct {
		name string
		expr string
	}{
		{name: "malformed", expr: "not a cron expr"},
		{name: "faster than scheduler resolution", expr: "@every 30s"},
		{name: "never fires", expr: "0 0 30 2 *"}, // Feb 30 does not exist
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := ParseSchedule(c.expr); err == nil {
				t.Errorf("%q: expected an error", c.expr)
			}
		})
	}
}
