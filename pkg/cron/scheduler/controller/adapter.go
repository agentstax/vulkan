package controller

import (
	"github.com/agentstax/vulkan/pkg/cron/scheduler/controller/datastore"
)

func toDueCronJob(data *datastore.DueCronJobData) *DueCronJob {
	return &DueCronJob{
		Id:              data.Id,
		Name:            data.Name,
		Schedule:        data.Schedule,
		Concurrency:     data.Concurrency,
		Timeout:         data.Timeout,
		Payload:         data.Payload,
		Metadata:        data.Metadata,
		NextScheduledAt: data.NextScheduledAt,
		DbNow:           data.DbNow,
	}
}
