package controller

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/cron"
	"github.com/agentstax/vulkan/pkg/cron/controller/datastore"
)

func toCronJob(data *datastore.CronJobData) (*cron.CronJob, error) {
	concurrency, err := concurrencyEnum(data.Concurrency)
	if err != nil {
		return nil, err
	}

	return &cron.CronJob{
		Id:                data.Id,
		SystemId:          data.SystemId,
		TopicId:           data.TopicId,
		ConsumerGroupId:   data.ConsumerGroupId,
		Name:              data.Name,
		Schedule:          data.Schedule,
		Concurrency:       concurrency,
		Timeout:           time.Duration(data.TimeoutNs),
		Suspended:         data.Suspended,
		Data:              data.Data,
		Metadata:          data.Metadata,
		NextScheduledTime: data.NextScheduledTime,
		LastScheduledTime: data.LastScheduledTime,
	}, nil
}

func toGroupStatus(data *datastore.GroupStatusData) *cron.GroupStatus {
	return &cron.GroupStatus{
		ConsumerGroup: data.ConsumerGroup,
		Ran:           data.Ran,
		Succeeded:     data.Succeeded,
		Superseded:    data.Superseded,
		Failed:        data.Failed,
	}
}

func toJobRequestStatus(data *datastore.JobRequestStatusData) (*cron.JobRequestStatus, error) {
	// ScheduledTime lives in the message payload, not a column
	var request cron.JobRequest
	if err := json.Unmarshal(data.Payload, &request); err != nil {
		return nil, fmt.Errorf("job request payload: %w", err)
	}

	status := &cron.JobRequestStatus{
		ConsumerGroup: data.ConsumerGroup,
		MessageId:     data.MessageId,
		ScheduledTime: request.ScheduledTime,
		ProducedAt:    data.ProducedAt,
		Outcome:       toJobRequestOutcome(data),
	}
	if status.Outcome == cron.JobRequestSuperseded {
		status.SupersededBy = data.SupersededBy
		status.SupersededAt = data.SupersededAt
	}
	return status, nil
}

func toJobRequestOutcome(data *datastore.JobRequestStatusData) cron.JobRequestOutcome {
	switch {
	case data.Succeeded:
		return cron.JobRequestSucceeded
	case data.Raised:
		return cron.JobRequestFailed
	// order specific - must be after Succeeded and Raised.
	// if it gets here the request never ran and a non-head
	// message can never run ie it was Superseded.
	case !data.Head:
		return cron.JobRequestSuperseded
	case data.Deferred:
		return cron.JobRequestDeferred
	default:
		return cron.JobRequestPending
	}
}

func toRegisterCronJobData(name string, schedule *cron.Schedule, data any, cfg *CronJobConfig) *datastore.RegisterCronJobData {
	return &datastore.RegisterCronJobData{
		Name:        name,
		Schedule:    schedule,
		Concurrency: string(cfg.Concurrency),
		TimeoutNs:   int64(cfg.Timeout),
		Data:        data,
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
