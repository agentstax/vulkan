package metrics

import (
	"time"

	"github.com/agentstax/vulkan/pkg/common"
)

// CronJobSnapshot is one cron_job row's schedule health.
type CronJobSnapshot struct {
	Owner     *common.Owner
	Name      string
	Schedule  string
	Suspended bool

	NextScheduledTime time.Time
	LastScheduledTime time.Time     // zero if the job has never been produced
	DueFor            time.Duration // now() - next_scheduled_time: negative until the slot arrives, positive while due and unproduced
	Overdue           bool          // due for longer than the overdue threshold and not suspended -- nothing is producing it
}
