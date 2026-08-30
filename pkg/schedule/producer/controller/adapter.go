package controller

import (
	"github.com/agentstax/vulkan/pkg/schedule/producer/controller/datastore"
)

func toDueSchedule(data *datastore.DueScheduleData) *DueSchedule {
	return &DueSchedule{
		Id:              data.Id,
		Name:            data.Name,
		Expression:      data.Expression,
		TopicName:       data.TopicName,
		Concurrency:     data.Concurrency,
		Timeout:         data.Timeout,
		Payload:         data.Payload,
		SchemaVersion:   data.SchemaVersion,
		Metadata:        data.Metadata,
		NextScheduledAt: data.NextScheduledAt,
		DbNow:           data.DbNow,
	}
}
