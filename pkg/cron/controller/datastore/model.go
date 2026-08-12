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
	Superseded    int64
	Failed        int64
}

// JobRequestStatusData is one (consumer group, job request) CronJobRequests row.
type JobRequestStatusData struct {
	ConsumerGroup string
	MessageId     int64
	Payload       json.RawMessage
	ProducedAt    time.Time
	Head          bool
	Succeeded     bool
	Raised        bool
	Deferred      bool
	SupersededBy  *int64
	SupersededAt  *time.Time
}

// matchingGroupData is one consumer group that receives a job's requests.
type matchingGroupData struct {
	Id   int64
	Name string
}

// jobMessageData is one of a job's message-log rows.
type jobMessageData struct {
	Id        int64
	Payload   json.RawMessage
	CreatedAt time.Time
}

// requestOutcomeData is one message's delivery history for one consumer
// group, rolled up to booleans. The zero value reads as "never delivered".
type requestOutcomeData struct {
	Succeeded bool
	Raised    bool
	Deferred  bool
}
