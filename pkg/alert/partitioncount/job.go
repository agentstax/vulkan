package partitioncount

import (
	"github.com/agentstax/vulkan/pkg/alert"
	"github.com/agentstax/vulkan/pkg/alert/partitioncount/controller"
	"github.com/agentstax/vulkan/pkg/common"
	croncontroller "github.com/agentstax/vulkan/pkg/cron/controller"
)

const JobName = "alert." + controller.AlertPartitionCount

// NewJob builds the cron job the partition_count alert is evaluated on.
// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
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
