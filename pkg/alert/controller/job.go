package controller

import (
	"errors"

	"github.com/agentstax/vulkan/pkg/alert"
	"github.com/agentstax/vulkan/pkg/schedule"
	schedulecontroller "github.com/agentstax/vulkan/pkg/schedule/controller"
)

// Job is one built-in alert's schedule, as RegisterSystem declares it.
type Job struct {
	Name       string
	Expression string
	Payload    *alert.JobPayload
	Config     *schedulecontroller.ScheduleConfig
}

// NewJob parses expression so a bad one fails here, not at register.
// cfg may be nil.
func NewJob(name string, expression string, payload *alert.JobPayload, cfg *schedulecontroller.ScheduleConfig) (*Job, error) {
	if name == "" {
		return nil, errors.New("job name is required")
	}
	if payload == nil {
		return nil, errors.New("job payload is required")
	}

	if _, err := schedule.ParseExpression(expression); err != nil {
		return nil, err
	}
	return &Job{
		Name:       name,
		Expression: expression,
		Payload:    payload,
		Config:     cfg,
	}, nil
}
