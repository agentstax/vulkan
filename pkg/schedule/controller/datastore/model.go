package datastore

import (
	"encoding/json"
	"time"

	"github.com/agentstax/vulkan/pkg/schedule"
)

// ScheduleConfigRow is one schedule_config row joined to its schedule_cursor row.
type ScheduleConfigRow struct {
	Id              int64           `db:"id"`
	SystemId        int64           `db:"system_id"`
	TopicId         int64           `db:"topic_id"`
	Name            string          `db:"name"`
	Expression      string          `db:"expression"`
	SchemaVersion   int             `db:"schema_version"`
	Concurrency     string          `db:"concurrency"`
	TimeoutNs       int64           `db:"timeout_ns"`
	Suspended       bool            `db:"suspended"`
	Payload         json.RawMessage `db:"payload"`
	Metadata        json.RawMessage `db:"metadata"`
	NextScheduledAt time.Time       `db:"next_scheduled_at"`
	LastScheduledAt *time.Time      `db:"last_scheduled_at"`
}

// ScheduleDeclaration is one declaration of a schedule, as RegisterSchedule
// takes it. Schedule stays parsed -- next_scheduled_at is computed from it.
type ScheduleDeclaration struct {
	Name          string
	Expression    *schedule.ScheduleExpression
	SystemId      int64
	TopicId       int64
	Concurrency   string
	TimeoutNs     int64
	Payload       any
	SchemaVersion int
	Metadata      any
}

// GroupStatus is one consumer group's Status counts.
type GroupStatus struct {
	ConsumerGroup string
	Ran           int64
	Succeeded     int64
	Superseded    int64
	Failed        int64
}

// MessageStatus is one (consumer group, message) ListMessages row.
type MessageStatus struct {
	ConsumerGroup string
	MessageId     int64
	ScheduledAt   time.Time
	ProducedAt    time.Time
	Head          bool
	Succeeded     bool
	Raised        bool
	Deferred      bool
	SupersededBy  *int64
	SupersededAt  *time.Time
}

// matchingGroupRow is one consumer group that receives a schedule's messages.
type matchingGroupRow struct {
	Id   int64  `db:"id"`
	Name string `db:"name"`
}

// keyMessageRow is one of a schedule's message-log rows.
type keyMessageRow struct {
	Id          int64     `db:"id"`
	ScheduledAt time.Time `db:"scheduled_at"` // options->>'scheduled_at'
	CreatedAt   time.Time `db:"created_at"`
}

// messageOutcomeRow is one message's delivery history for one consumer
// group, rolled up to booleans. The zero value reads as "never delivered".
type messageOutcomeRow struct {
	Succeeded bool `db:"succeeded"`
	Raised    bool `db:"raised"`
	Deferred  bool `db:"deferred"`
}
