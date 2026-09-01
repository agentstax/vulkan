package workerliveness

import (
	"github.com/agentstax/vulkan/pkg/alert"
	alertcontroller "github.com/agentstax/vulkan/pkg/alert/controller"
	"github.com/agentstax/vulkan/pkg/alert/workerliveness/controller"
	"github.com/agentstax/vulkan/pkg/common"
	schedulecontroller "github.com/agentstax/vulkan/pkg/schedule/controller"
)

const JobName = "alert." + controller.AlertWorkerLiveness

// NewJob builds the schedule the worker_liveness alert is evaluated on.
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

	// the payload's threshold is the shared job shape; Evaluate ignores it
	data, err := alert.NewJobPayload(0)
	if err != nil {
		return nil, err
	}

	// exclusive so runs never overlap
	return alertcontroller.NewJob(JobName, cfg.Expression, data, &schedulecontroller.ScheduleConfig{Concurrency: common.ConcurrencyExclusive})
}
