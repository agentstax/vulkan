package controller

import (
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
		Failed:        data.Failed,
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

func toAlterCronJobData(cfg *AlterCronJobConfig) *datastore.AlterCronJobData {
	return &datastore.AlterCronJobData{
		Schedule:    cfg.Schedule,
		Concurrency: concurrencyString(cfg.Concurrency),
		TimeoutNs:   durationNs(cfg.Timeout),
		Data:        cfg.Data,
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

func concurrencyString(concurrency common.ConcurrencyPolicy) *string {
	if concurrency == "" {
		return nil
	}
	s := string(concurrency)
	return &s
}

// durationNs widens *time.Duration to the *int64 the _ns columns store,
// passing nil through so COALESCE sees NULL.
func durationNs(d *time.Duration) *int64 {
	if d == nil {
		return nil
	}
	ns := int64(*d)
	return &ns
}
