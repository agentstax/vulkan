package compactionreadcost

import (
	"github.com/agentstax/vulkan/pkg/alert"
	"github.com/agentstax/vulkan/pkg/alert/compactionreadcost/controller"
	alertcontroller "github.com/agentstax/vulkan/pkg/alert/controller"
	"github.com/agentstax/vulkan/pkg/common"
	schedulecontroller "github.com/agentstax/vulkan/pkg/schedule/controller"
)

const JobName = "alert." + controller.AlertCompactionReadCost

// NewJob builds the schedule the compaction_read_cost alert is evaluated on.
// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewJob(cfg *JobConfig) (*alertcontroller.Job, error) {
	if cfg == nil {
		cfg = &JobConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	data, err := alert.NewJobPayload(cfg.Threshold)
	if err != nil {
		return nil, err
	}

	// exclusive so runs never overlap
	return alertcontroller.NewJob(JobName, cfg.Expression, data, &schedulecontroller.ScheduleConfig{Concurrency: common.ConcurrencyExclusive})
}
