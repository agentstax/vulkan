package controller

import (
	"errors"

	"github.com/agentstax/vulkan/pkg/alert"
	"github.com/agentstax/vulkan/pkg/cron"
	croncontroller "github.com/agentstax/vulkan/pkg/cron/controller"
)

// Job is one built-in alert's cron job, as RegisterSystem declares it.
type Job struct {
	Name     string
	Schedule *cron.Schedule
	Payload  *alert.JobPayload
	Config   *croncontroller.CronJobConfig
}

// NewJob parses schedule so a bad expression fails here, not at register.
// cfg may be nil.
func NewJob(name string, schedule string, payload *alert.JobPayload, cfg *croncontroller.CronJobConfig) (*Job, error) {
	if name == "" {
		return nil, errors.New("job name is required")
	}
	if payload == nil {
		return nil, errors.New("job payload is required")
	}

	parsed, err := cron.ParseSchedule(schedule)
	if err != nil {
		return nil, err
	}
	return &Job{
		Name:     name,
		Schedule: parsed,
		Payload:  payload,
		Config:   cfg,
	}, nil
}
