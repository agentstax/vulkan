package controller

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/schedule"
	"github.com/agentstax/vulkan/pkg/schedule/controller/datastore"
)

func toSchedule(data *datastore.ScheduleData) (*schedule.Schedule, error) {
	concurrency, err := concurrencyEnum(data.Concurrency)
	if err != nil {
		return nil, err
	}

	return &schedule.Schedule{
		Id:              data.Id,
		SystemId:        data.SystemId,
		TopicId:         data.TopicId,
		ConsumerGroupId: data.ConsumerGroupId,
		Name:            data.Name,
		Expression:      data.Expression,
		Concurrency:     concurrency,
		Timeout:         time.Duration(data.TimeoutNs),
		Suspended:       data.Suspended,
		Payload:         data.Payload,
		Metadata:        data.Metadata,
		NextScheduledAt: data.NextScheduledAt,
		LastScheduledAt: data.LastScheduledAt,
	}, nil
}

func toGroupStatus(data *datastore.GroupStatusData) *schedule.GroupStatus {
	return &schedule.GroupStatus{
		ConsumerGroup: data.ConsumerGroup,
		Ran:           data.Ran,
		Succeeded:     data.Succeeded,
		Superseded:    data.Superseded,
		Failed:        data.Failed,
	}
}

func toJobRequestStatus(data *datastore.JobRequestStatusData) (*schedule.JobRequestStatus, error) {
	// ScheduledAt lives in the message payload, not a column
	var request schedule.JobRequest
	if err := json.Unmarshal(data.Payload, &request); err != nil {
		return nil, fmt.Errorf("job request payload: %w", err)
	}

	status := &schedule.JobRequestStatus{
		ConsumerGroup: data.ConsumerGroup,
		MessageId:     data.MessageId,
		ScheduledAt:   request.ScheduledAt,
		ProducedAt:    data.ProducedAt,
		Outcome:       toJobRequestOutcome(data),
	}
	if status.Outcome == schedule.JobRequestSuperseded {
		status.SupersededBy = data.SupersededBy
		status.SupersededAt = data.SupersededAt
	}
	return status, nil
}

func toJobRequestOutcome(data *datastore.JobRequestStatusData) schedule.JobRequestOutcome {
	switch {
	case data.Succeeded:
		return schedule.JobRequestSucceeded
	case data.Raised:
		return schedule.JobRequestFailed
	// order specific - must be after Succeeded and Raised.
	// if it gets here the request never ran and a non-head
	// message can never run ie it was Superseded.
	case !data.Head:
		return schedule.JobRequestSuperseded
	case data.Deferred:
		return schedule.JobRequestDeferred
	default:
		return schedule.JobRequestPending
	}
}

func toRegisterScheduleData(name string, expression *schedule.Expression, payload any, cfg *ScheduleConfig) *datastore.RegisterScheduleData {
	return &datastore.RegisterScheduleData{
		Name:        name,
		Expression:  expression,
		Concurrency: string(cfg.Concurrency),
		TimeoutNs:   int64(cfg.Timeout),
		Payload:     payload,
		Metadata:    cfg.Metadata,
	}
}

func concurrencyEnum(concurrency string) (common.ConcurrencyPolicy, error) {
	policy := common.ConcurrencyPolicy(concurrency)
	if err := policy.Validate(); err != nil {
		return "", fmt.Errorf("stored concurrency: %w", err)
	}
	return policy, nil
}
