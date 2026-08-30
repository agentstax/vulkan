// Vendored from github.com/robfig/cron/v3 v3.0.1 (MIT -- see LICENSE in this directory).
// Hoisted from upstream's cron.go, which is not vendored (their in-process runner).

package robfig

import "time"

// Schedule describes a job's duty cycle.
type Schedule interface {
	// Next returns the next activation time, later than the given time.
	// Next is invoked initially, and then each time the job is run.
	Next(time.Time) time.Time
}
