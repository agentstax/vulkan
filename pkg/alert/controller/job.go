package controller

import (
	"errors"

	"github.com/agentstax/vulkan/pkg/alert"
	"github.com/agentstax/vulkan/pkg/schedule"
)

// Job is one built-in alert's schedule, as RegisterSystem declares it.
type Job struct {
	Name    string
	Cron    string
	Payload *alert.JobPayload
}

// NewJob parses expression so a bad one fails here, not at register.
func NewJob(name string, expression string, payload *alert.JobPayload) (*Job, error) {
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
		Name:    name,
		Cron:    expression,
		Payload: payload,
	}, nil
}
