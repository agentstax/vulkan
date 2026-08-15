package compactionreadcost

import (
	"github.com/agentstax/vulkan/pkg/alert"
	"github.com/agentstax/vulkan/pkg/alert/compactionreadcost/controller"
	"github.com/agentstax/vulkan/pkg/common"
	croncontroller "github.com/agentstax/vulkan/pkg/cron/controller"
)

const JobName = "alert." + controller.AlertCompactionReadCost

func NewJob(cfg *JobConfig) (*alert.Job, error) {
	if cfg == nil {
		cfg = &JobConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	data, err := alert.NewJobData(cfg.Threshold)
	if err != nil {
		return nil, err
	}
	// concurrency defers so runs never overlap
	return alert.NewJob(JobName, cfg.Schedule, data, &croncontroller.CronJobConfig{Concurrency: common.ConcurrencyDefer})
}
