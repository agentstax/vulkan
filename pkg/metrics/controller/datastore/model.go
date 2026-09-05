package datastore

import (
	"time"
)

// WorkerSnapshotRow is one row of WorkerSnapshots' query: the worker row's
// owner columns plus its aggregated worker_instance liveness.
type WorkerSnapshotRow struct {
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

// ScheduleSnapshotRow is one row of ScheduleSnapshots' query: the schedule
// row, its target topic, and its schedule state.
type ScheduleSnapshotRow struct {
	Name      string `db:"name"`
	SystemId  int64  `db:"system_id"`
	TopicId   int64  `db:"topic_id"`
	TopicName string `db:"topic_name"`

	Expression      string     `db:"expression"`
	Suspended       bool       `db:"suspended"`
	NextScheduledAt time.Time  `db:"next_scheduled_at"`
	LastScheduledAt *time.Time `db:"last_scheduled_at"` // NULL until the job's first produced request
	DueForSecs      float64    `db:"due_for_secs"`
}

// ConsumerGroupIdentityRow is one group's id and name used by TopicSnapshot.
type ConsumerGroupIdentityRow struct {
	Id   int64  `db:"id"`
	Name string `db:"name"`
}

// ConsumerGroupSnapshotRow is one (group, topic)'s cursor row plus the
// counted delivery/lease state around it.
type ConsumerGroupSnapshotRow struct {
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

// EventTimestampRow is one row of EventTimestamps' query: a distinct
// (message, attempt) and the earliest time its event type was seen.
type EventTimestampRow struct {
	MessageId int64     `db:"message_id"`
	Attempt   int       `db:"attempt"`
	At        time.Time `db:"at"`
}

// SchemaVersionCountRow is one row of SchemaVersionCounts' query: a payload
// version present in the log, its row count, and the compaction heads at it.
type SchemaVersionCountRow struct {
	SchemaVersion   int64 `db:"schema_version"`
	Messages        int64 `db:"messages"`
	CompactionHeads int64 `db:"compaction_heads"`
}

// ConsumerGroupSchemaVersionLagRow is one row of ConsumerGroupSchemaVersionLag's query: a
// group's unread and unresolved rows at one payload version.
type ConsumerGroupSchemaVersionLagRow struct {
	ConsumerGroup        string `db:"consumer_group"`
	Unconsumed           int64  `db:"unconsumed"`
	UnresolvedExceptions int64  `db:"unresolved_exceptions"`
}
