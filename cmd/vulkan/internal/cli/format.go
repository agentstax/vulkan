package cli

import (
	"fmt"
	"strconv"
	"time"
)

// commaInt renders 1000000 as "1,000,000". Only PartitionSize gets grouping;
// everything else (batch sizes, ids) prints bare, matching ADMIN_CLI.md.
func commaInt(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := ""
	if len(s) > 0 && s[0] == '-' {
		neg, s = "-", s[1:]
	}
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	return neg + string(out)
}

// compactDuration is the list-column form: "720h", "1h", "5s" -- collapse to the
// coarsest whole unit, fall back to Go's duration string for odd values.
func compactDuration(d time.Duration) string {
	switch {
	case d == 0:
		return "0s"
	case d%time.Hour == 0:
		return fmt.Sprintf("%dh", d/time.Hour)
	case d%time.Minute == 0:
		return fmt.Sprintf("%dm", d/time.Minute)
	default:
		return d.String()
	}
}

// dayParenthetical adds " (30d)" when a duration is a whole number of days.
func dayParenthetical(d time.Duration) string {
	const day = 24 * time.Hour
	if d > 0 && d%day == 0 {
		return fmt.Sprintf(" (%dd)", d/day)
	}
	return ""
}

// retentionCell is the list RETENTION column: "forever" for keep-indefinitely,
// else the compact form plus a day parenthetical.
func retentionCell(d time.Duration) string {
	if d == 0 {
		return "forever"
	}
	return compactDuration(d) + dayParenthetical(d)
}

// retentionDetail is the get form: raw Go duration string ("720h0m0s"), or
// "forever" for keep-indefinitely.
func retentionDetail(d time.Duration) string {
	if d == 0 {
		return "forever"
	}
	return d.String()
}

// timeCell renders a topic timestamp (created/updated) for the list/get views,
// to the minute, in whatever zone the driver returns it in.
func timeCell(t time.Time) string {
	return t.Format("2006-01-02 15:04")
}

// pluralize - "1 topic" / "2 topics".
func pluralize(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
