package datastore

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// WorkerSnapshotData is one row of WorkerSnapshots' query: the worker row's
// owner columns plus its aggregated worker_instance liveness.
type WorkerSnapshotData struct {
	Name            string
	SystemId        int64
	TopicId         int64
	ConsumerGroupId int64
	TopicName       string
	GroupName       string

	TargetInstances       int
	LiveInstances         int
	MaxAttempts           int
	OldestInstanceAgeSecs float64
	UnclaimedForSecs      float64
}

// CronJobSnapshotData is one row of CronJobSnapshots' query: the cron_job
// row's owner columns plus its schedule state.
type CronJobSnapshotData struct {
	Name            string
	SystemId        int64
	TopicId         int64
	ConsumerGroupId int64
	TopicName       string
	GroupName       string

	Schedule          string
	Suspended         bool
	NextScheduledTime time.Time
	LastScheduledTime pgtype.Timestamptz // NULL until the job's first produced request
	DueForSecs        float64
}

// ConsumerGroupSnapshotData is one (group, topic)'s cursor row plus the
// counted delivery/lease state around it.
type ConsumerGroupSnapshotData struct {
	Claimed            int64
	Committed          int64
	Head               int64
	ReadyExceptions    int64
	InflightExceptions int64
	DeferredExceptions int64
	DeadExceptions     int64
	OldestUnresolvedAt *time.Time // NULL with no ready/inflight/deferred row
	OpenLeases         int64
}

// EventTimestampData is one row of EventTimestamps' query: a distinct
// (message, attempt) and the earliest time its event type was seen.
type EventTimestampData struct {
	MessageId int64
	Attempt   int
	At        time.Time
}
