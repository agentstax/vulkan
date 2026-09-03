package controller

import (
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/schedule"
	"github.com/agentstax/vulkan/pkg/schedule/controller/datastore"
)

func toScheduleData(data *datastore.ScheduleConfigRow) (*schedule.ScheduleData, error) {
	concurrency, err := concurrencyEnum(data.Concurrency)
	if err != nil {
		return nil, err
	}

	return &schedule.ScheduleData{
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

func toGroupStatus(data *datastore.GroupStatus) *schedule.GroupStatus {
	return &schedule.GroupStatus{
		ConsumerGroup: data.ConsumerGroup,
		Ran:           data.Ran,
		Succeeded:     data.Succeeded,
		Superseded:    data.Superseded,
		Failed:        data.Failed,
	}
}

func toMessageStatus(data *datastore.MessageStatus) *schedule.MessageStatus {
	status := &schedule.MessageStatus{
		ConsumerGroup: data.ConsumerGroup,
		MessageId:     data.MessageId,
		ScheduledAt:   data.ScheduledAt,
		ProducedAt:    data.ProducedAt,
		Outcome:       toMessageOutcome(data),
	}
	if status.Outcome == schedule.MessageSuperseded {
		status.SupersededBy = data.SupersededBy
		status.SupersededAt = data.SupersededAt
	}
	return status
}

func toMessageOutcome(data *datastore.MessageStatus) schedule.MessageOutcome {
	switch {
	case data.Succeeded:
		return schedule.MessageSucceeded
	case data.Raised:
		return schedule.MessageFailed
	// order specific - must be after Succeeded and Raised.
	// if it gets here the request never ran and a non-head
	// message can never run ie it was Superseded.
	case !data.Head:
		return schedule.MessageSuperseded
	case data.Deferred:
		return schedule.MessageDeferred
	default:
		return schedule.MessagePending
	}
}

func toScheduleDeclaration[Message common.Versioned](systemId int64, name string, expression *schedule.Expression, topicId int64, payload *Message, cfg *schedule.ScheduleConfig) *datastore.ScheduleDeclaration {
	return &datastore.ScheduleDeclaration{
		Name:          name,
		Expression:    expression,
		SystemId:      systemId,
		TopicId:       topicId,
		Concurrency:   string(cfg.Concurrency),
		TimeoutNs:     int64(cfg.Timeout),
		Payload:       payload,
		SchemaVersion: common.SchemaVersionOf[Message](),
		Metadata:      cfg.Metadata,
	}
}

func concurrencyEnum(concurrency string) (common.ConcurrencyPolicy, error) {
	policy := common.ConcurrencyPolicy(concurrency)
	if err := policy.Validate(); err != nil {
		return "", fmt.Errorf("stored concurrency: %w", err)
	}
	return policy, nil
}
