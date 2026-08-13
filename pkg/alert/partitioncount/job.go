package partitioncount

import (
	"github.com/agentstax/vulkan/pkg/alert"
	"github.com/agentstax/vulkan/pkg/alert/partitioncount/controller"
	"github.com/agentstax/vulkan/pkg/common"
	croncontroller "github.com/agentstax/vulkan/pkg/cron/controller"
)

const JobName = "alert." + controller.AlertPartitionCount

const schedule = "@hourly"

func NewJob() (*alert.Job, error) {
	data, err := alert.NewJobData(0)
	if err != nil {
		return nil, err
	}
	// concurrency defers so runs never overlap
	return alert.NewJob(JobName, schedule, data, &croncontroller.CronJobConfig{Concurrency: common.ConcurrencyDefer})
}
