package metrics

import (
	"time"

	"github.com/agentstax/vulkan/pkg/common"
)

// CronJobSnapshot is one cron_job row's schedule health.
type CronJobSnapshot struct {
	Owner     *common.Owner `json:"owner"`
	Name      string        `json:"cron_job"`
	Schedule  string        `json:"schedule"`
	Suspended bool          `json:"suspended"`

	NextScheduledTime time.Time     `json:"next_scheduled_time"`
	LastScheduledTime time.Time     `json:"last_scheduled_time"` // zero if the job has never been produced
	DueFor            time.Duration `json:"due_for"`             // now() - next_scheduled_time: negative until the slot arrives, positive while due and unproduced
	Overdue           bool          `json:"overdue"`             // due for longer than the overdue threshold and not suspended -- nothing is producing it
}
