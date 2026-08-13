package partitioncount

import (
	"github.com/agentstax/vulkan/pkg/alert"
	"github.com/agentstax/vulkan/pkg/common"
	croncontroller "github.com/agentstax/vulkan/pkg/cron/controller"
)

const AlertPartitionCount = "partition_count"

const JobName = "alert." + AlertPartitionCount

const schedule = "@hourly"

// warnDivisor halves the lock ceiling so the alert leaves headroom to act
// before Destroy starts failing.
const warnDivisor = 2

func NewJob() (*alert.Job, error) {
	data, err := alert.NewJobData(0)
	if err != nil {
		return nil, err
	}
	// concurrency defers so runs never overlap
	return alert.NewJob(JobName, schedule, data, &croncontroller.CronJobConfig{Concurrency: common.ConcurrencyDefer})
}
