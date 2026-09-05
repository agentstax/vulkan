package partitioncount

import (
	"github.com/agentstax/vulkan/pkg/alert"
	alertcontroller "github.com/agentstax/vulkan/pkg/alert/controller"
)

var JobName = "alert." + alert.AlertPartitionCount.Name

// NewJob builds the schedule the partition_count alert is evaluated on.
// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewJob(cfg *alert.PartitionCountJobConfig) (*alertcontroller.Job, error) {
	if cfg == nil {
		cfg = &alert.PartitionCountJobConfig{}
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
	return alertcontroller.NewJob(JobName, cfg.Expression, data)
}
