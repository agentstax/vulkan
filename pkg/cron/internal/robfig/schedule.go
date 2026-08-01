// Vendored from github.com/robfig/cron/v3 v3.0.1 (MIT -- see LICENSE in this directory).
// This declaration is hoisted out of upstream's cron.go, which is otherwise
// not vendored -- it is their in-process runner, and the scheduler duty is
// ours.

package robfig

import "time"

// Schedule describes a job's duty cycle.
type Schedule interface {
	// Next returns the next activation time, later than the given time.
	// Next is invoked initially, and then each time the job is run.
	Next(time.Time) time.Time
}
