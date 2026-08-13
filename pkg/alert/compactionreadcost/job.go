package compactionreadcost

import (
	"github.com/agentstax/vulkan/pkg/alert"
	"github.com/agentstax/vulkan/pkg/common"
	croncontroller "github.com/agentstax/vulkan/pkg/cron/controller"
)

const AlertCompactionReadCost = "compaction_read_cost"

const JobName = "alert." + AlertCompactionReadCost

const schedule = "@hourly"

// warnPartitions is where one never-superseded key's replay, at ~10µs per
// partition, crosses ~100ms.
const warnPartitions = 10_000

func NewJob() (*alert.Job, error) {
	data, err := alert.NewJobData(0)
	if err != nil {
		return nil, err
	}
	// concurrency defers so runs never overlap
	return alert.NewJob(JobName, schedule, data, &croncontroller.CronJobConfig{Concurrency: common.ConcurrencyDefer})
}
