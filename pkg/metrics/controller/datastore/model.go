package datastore

import (
	"time"
)

// WorkerSnapshotData is one row of WorkerSnapshots' query: the worker row's
// owner columns plus its aggregated worker_instance liveness.
type WorkerSnapshotData struct {
	Name            string `db:"name"`
	SystemId        int64  `db:"system_id"`
	TopicId         int64  `db:"topic_id"`
	ConsumerGroupId int64  `db:"consumer_group_id"`
	TopicName       string `db:"topic_name"`
	GroupName       string `db:"group_name"`

	TargetInstances  int     `db:"target_instances"`
	LiveInstances    int     `db:"live_instances"`
	MaxAttempts      int     `db:"max_attempts"`
	UnclaimedForSecs float64 `db:"unclaimed_for_secs"`
}

// CronJobSnapshotData is one row of CronJobSnapshots' query: the cron_job
// row's owner columns plus its schedule state.
type CronJobSnapshotData struct {
	Name            string `db:"name"`
	SystemId        int64  `db:"system_id"`
	TopicId         int64  `db:"topic_id"`
	ConsumerGroupId int64  `db:"consumer_group_id"`
	TopicName       string `db:"topic_name"`
	GroupName       string `db:"group_name"`

	Schedule          string     `db:"schedule"`
	Suspended         bool       `db:"suspended"`
	NextScheduledTime time.Time  `db:"next_scheduled_time"`
	LastScheduledTime *time.Time `db:"last_scheduled_time"` // NULL until the job's first produced request
	DueForSecs        float64    `db:"due_for_secs"`
}

// ConsumerGroupSnapshotData is one (group, topic)'s cursor row plus the
// counted delivery/lease state around it.
type ConsumerGroupSnapshotData struct {
	Claimed            int64      `db:"claimed"`
	Committed          int64      `db:"committed"`
	Head               int64      `db:"head"`
	ReadyExceptions    int64      `db:"ready_exceptions"`
	InflightExceptions int64      `db:"inflight_exceptions"`
	DeferredExceptions int64      `db:"deferred_exceptions"`
	DeadExceptions     int64      `db:"dead_exceptions"`
	OldestUnresolvedAt *time.Time `db:"oldest_unresolved_at"` // NULL with no ready/inflight/deferred row
	OpenLeases         int64      `db:"open_leases"`
}

// EventTimestampData is one row of EventTimestamps' query: a distinct
// (message, attempt) and the earliest time its event type was seen.
type EventTimestampData struct {
	MessageId int64     `db:"message_id"`
	Attempt   int       `db:"attempt"`
	At        time.Time `db:"at"`
}
