package metrics

import (
	"time"

	"github.com/agentstax/vulkan/pkg/common"
)

// CronJobSnapshot is one cron_job row's firing health.
type CronJobSnapshot struct {
	Owner     *common.Owner
	Name      string
	Schedule  string
	Suspended bool

	NextScheduledTime time.Time
	LastScheduledTime time.Time     // zero if the job has never fired
	DueFor            time.Duration // now() - next_scheduled_time: negative until the slot arrives, positive while due and unfired
	Overdue           bool          // due for longer than the overdue threshold and not suspended -- nothing is firing it
}
