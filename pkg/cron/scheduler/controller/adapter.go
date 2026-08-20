package controller

import (
	"github.com/agentstax/vulkan/pkg/cron/scheduler/controller/datastore"
)

func toDueCronJob(data *datastore.DueCronJobData) *DueCronJob {
	return &DueCronJob{
		Id:                data.Id,
		Name:              data.Name,
		Schedule:          data.Schedule,
		Concurrency:       data.Concurrency,
		Timeout:           data.Timeout,
		Data:              data.Data,
		Metadata:          data.Metadata,
		NextScheduledTime: data.NextScheduledTime,
		DbNow:             data.DbNow,
	}
}
