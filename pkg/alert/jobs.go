package alert

import (
	"encoding/json"
	"fmt"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/cron"
)

// Job is one check's cron job -- what RegisterSystem seeds and Run executes.
type Job struct {
	Name     string
	Schedule *cron.Schedule
	Data     any
	Config   *cron.Config
}

func NewJob(name string, schedule string, data any, cfg *cron.Config) (*Job, error) {
	if name == "" {
		return nil, fmt.Errorf("job name is required")
	}
	sched, err := cron.ParseSchedule(schedule)
	if err != nil {
		return nil, err
	}
	return &Job{
		Name:     name,
		Schedule: sched,
		Data:     data,
		Config:   cfg,
	}, nil
}

// Jobs builds every check's job. A check never overlaps itself, so each job
// defers.
func Jobs() ([]*Job, error) {
	partitionCount, err := newCheckJob(PartitionCountJobName, partitionCountSchedule)
	if err != nil {
		return nil, err
	}
	compactionReadCost, err := newCheckJob(CompactionReadCostJobName, compactionReadCostSchedule)
	if err != nil {
		return nil, err
	}
	return []*Job{partitionCount, compactionReadCost}, nil
}

func newCheckJob(name, schedule string) (*Job, error) {
	data, err := newJobData(0)
	if err != nil {
		return nil, err
	}
	return NewJob(name, schedule, data, &cron.Config{Concurrency: common.ConcurrencyDefer})
}

// jobData is a check job's data payload.
type jobData struct {
	Threshold int64 `json:"threshold"`
}

func newJobData(threshold int64) (*jobData, error) {
	if threshold < 0 {
		return nil, fmt.Errorf("threshold must be >= 0, got %d", threshold)
	}
	return &jobData{Threshold: threshold}, nil
}

// threshold decodes a job request's data payload. 0 means the check derives its
// threshold live, or uses its default.
func threshold(data json.RawMessage) (int64, error) {
	var d jobData
	if len(data) > 0 {
		if err := json.Unmarshal(data, &d); err != nil {
			return 0, err
		}
	}
	if d.Threshold < 0 {
		return 0, fmt.Errorf("threshold must be >= 0, got %d", d.Threshold)
	}
	return d.Threshold, nil
}
