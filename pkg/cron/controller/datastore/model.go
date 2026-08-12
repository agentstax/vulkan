package datastore

import (
	"encoding/json"
	"time"

	"github.com/agentstax/vulkan/pkg/cron"
)

// CronJobData models the cron_job table row exactly -- the nullable owner id
// columns scan COALESCE'd to 0.
type CronJobData struct {
	Id                int64
	SystemId          int64
	TopicId           int64
	ConsumerGroupId   int64
	Name              string
	Schedule          string
	Concurrency       string
	TimeoutNs         int64
	Suspended         bool
	Data              json.RawMessage
	Metadata          json.RawMessage
	NextScheduledTime time.Time
	LastScheduledTime *time.Time
}

// RegisterCronJobData is RegisterCronJob's insert input. Schedule stays parsed
// -- the insert computes next_scheduled_time from it.
type RegisterCronJobData struct {
	Name        string
	Schedule    *cron.Schedule
	Concurrency string
	TimeoutNs   int64
	Data        any
	Metadata    any
}

// AlterCronJobData is UpdateCronJob's sparse patch -- a nil field means leave
// the column unchanged.
type AlterCronJobData struct {
	Schedule    *cron.Schedule
	Concurrency *string
	TimeoutNs   *int64
	Data        any
	Metadata    any
}

// GroupStatusData is one consumer group's CronJobStatus counts.
type GroupStatusData struct {
	ConsumerGroup string
	Ran           int64
	Succeeded     int64
	Failed        int64
}
