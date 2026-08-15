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

// latencyCell renders an average self-clear latency: "-" when nothing has
// ever cleared (zero is not yet a measurement), sub-second kept to
// millisecond detail, anything larger rounds to seconds.
func latencyCell(d time.Duration) string {
	if d == 0 {
		return "-"
	}
	if d.Abs() < time.Second {
		return d.Round(time.Millisecond).String()
	}
	return d.Round(time.Second).String()
}
