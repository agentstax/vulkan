package controller

import (
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/schedule"
	"github.com/agentstax/vulkan/pkg/schedule/controller/datastore"
)

func toSchedule(data *datastore.ScheduleConfigRow) (*schedule.Schedule, error) {
	concurrency, err := concurrencyEnum(data.Concurrency)
	if err != nil {
		return nil, err
	}

	return &schedule.Schedule{
		Id:              data.Id,
		SystemId:        data.SystemId,
		TopicId:         data.TopicId,
		Name:            data.Name,
		Expression:      data.Expression,
		SchemaVersion:   data.SchemaVersion,
		Concurrency:     concurrency,
		Timeout:         time.Duration(data.TimeoutNs),
		Suspended:       data.Suspended,
		Payload:         data.Payload,
		Metadata:        data.Metadata,
		NextScheduledAt: data.NextScheduledAt,
		LastScheduledAt: data.LastScheduledAt,
	}, nil
}

func toScheduleGroupSummary(data *datastore.ScheduleGroupSummaryRow) *schedule.ScheduleGroupSummary {
	return &schedule.ScheduleGroupSummary{
		ConsumerGroup: data.ConsumerGroup,
		Ran:           data.Ran,
		Succeeded:     data.Succeeded,
		Superseded:    data.Superseded,
		Failed:        data.Failed,
	}
}

func toScheduleMessageStatus(data *datastore.ScheduleMessageStatusRow) *schedule.ScheduleMessageStatus {
	status := &schedule.ScheduleMessageStatus{
		ConsumerGroup: data.ConsumerGroup,
		MessageId:     data.MessageId,
		ScheduledAt:   data.ScheduledAt,
		ProducedAt:    data.ProducedAt,
		Outcome:       toScheduleMessageOutcome(data),
	}
	if status.Outcome == schedule.ScheduleMessageSuperseded {
		status.SupersededBy = data.SupersededBy
		status.SupersededAt = data.SupersededAt
	}
	return status
}

func toScheduleMessageOutcome(data *datastore.ScheduleMessageStatusRow) schedule.ScheduleMessageOutcome {
	switch {
	case data.Succeeded:
		return schedule.ScheduleMessageSucceeded
	case data.Raised:
		return schedule.ScheduleMessageFailed
	// order specific - must be after Succeeded and Raised.
	// if it gets here the request never ran and a non-head
	// message can never run ie it was Superseded.
	case !data.Head:
		return schedule.ScheduleMessageSuperseded
	case data.Deferred:
		return schedule.ScheduleMessageDeferred
	default:
		return schedule.ScheduleMessagePending
	}
}

func concurrencyEnum(concurrency string) (common.ConcurrencyPolicy, error) {
	policy := common.ConcurrencyPolicy(concurrency)
	if err := policy.Validate(); err != nil {
		return "", fmt.Errorf("stored concurrency: %w", err)
	}
	return policy, nil
}
