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
	Expression *schedule.Expression
	Payload    *alert.JobPayload
	Config     *schedulecontroller.ScheduleConfig
}

// NewJob parses schedule so a bad expression fails here, not at register.
// cfg may be nil.
func NewJob(name string, expression string, payload *alert.JobPayload, cfg *schedulecontroller.ScheduleConfig) (*Job, error) {
	if name == "" {
		return nil, errors.New("job name is required")
	}
	if payload == nil {
		return nil, errors.New("job payload is required")
	}

	parsed, err := schedule.ParseExpression(expression)
	if err != nil {
		return nil, err
	}
	return &Job{
		Name:       name,
		Expression: parsed,
		Payload:    payload,
		Config:     cfg,
	}, nil
}
