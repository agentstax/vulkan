package alert

import (
	"errors"

	"github.com/agentstax/vulkan/pkg/cron"
	croncontroller "github.com/agentstax/vulkan/pkg/cron/controller"
)

type Job struct {
	Name     string
	Schedule *cron.Schedule
	Data     *JobData
	Config   *croncontroller.CronJobConfig
}

func NewJob(name string, schedule string, data *JobData, cfg *croncontroller.CronJobConfig) (*Job, error) {
	if name == "" {
		return nil, errors.New("job name is required")
	}
	if data == nil {
		return nil, errors.New("job data is required")
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
