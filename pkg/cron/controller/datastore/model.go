package datastore

import (
	"encoding/json"
	"time"

	"github.com/agentstax/vulkan/pkg/cron"
)

// CronJobData models the cron_job table row exactly -- the nullable owner id
// columns scan COALESCE'd to 0.
type CronJobData struct {
	Id                int64           `db:"id"`
	SystemId          int64           `db:"system_id"`
	TopicId           int64           `db:"topic_id"`
	ConsumerGroupId   int64           `db:"consumer_group_id"`
	Name              string          `db:"name"`
	Schedule          string          `db:"schedule"`
	Concurrency       string          `db:"concurrency"`
	TimeoutNs         int64           `db:"timeout_ns"`
	Suspended         bool            `db:"suspended"`
	Data              json.RawMessage `db:"data"`
	Metadata          json.RawMessage `db:"metadata"`
	NextScheduledTime time.Time       `db:"next_scheduled_time"`
	LastScheduledTime *time.Time      `db:"last_scheduled_time"`
}

// RegisterCronJobData is one declaration of a cron job, as RegisterCronJob
// takes it. Schedule stays parsed -- next_scheduled_time is computed from it.
type RegisterCronJobData struct {
	Name        string
	Schedule    *cron.Schedule
	Concurrency string
	TimeoutNs   int64
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
	Id   int64  `db:"id"`
	Name string `db:"name"`
}

// jobMessageData is one of a job's message-log rows.
type jobMessageData struct {
	Id        int64           `db:"id"`
	Payload   json.RawMessage `db:"payload"`
	CreatedAt time.Time       `db:"created_at"`
}

// requestOutcomeData is one message's delivery history for one consumer
// group, rolled up to booleans. The zero value reads as "never delivered".
type requestOutcomeData struct {
	Succeeded bool `db:"succeeded"`
	Raised    bool `db:"raised"`
	Deferred  bool `db:"deferred"`
}
