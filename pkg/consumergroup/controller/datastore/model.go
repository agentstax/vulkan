package datastore

import "time"

type GroupData struct {
	Id        int64     `db:"id"`
	TopicId   int64     `db:"topic_id"`
	Name      string    `db:"name"`
	CreatedAt time.Time `db:"created_at"`
}

// BindingLogStatus is the binding_log.status column's value set.
type BindingLogStatus string

const (
	BindingLogInstalled BindingLogStatus = "installed" // a set change that took effect
	BindingLogWaiting   BindingLogStatus = "waiting"   // an attempt blocked by a live instance's different set
)

// BindingLogData is one binding_log row joined to the names
// a listing shows.
type BindingLogData struct {
	Id              int64                    `db:"id"`
	ConsumerGroupId int64                    `db:"consumer_group_id"`
	GroupName       string                   `db:"group_name"`
	TopicName       string                   `db:"topic_name"`
	SchemaVersion   int64                    `db:"schema_version"`
	Status          BindingLogStatus `db:"status"`
	Patterns        []string                 `db:"patterns"`
	DeclaredBy      string                   `db:"declared_by"`
	DeclaredAt      time.Time                `db:"declared_at"`
	AttemptAt       time.Time                `db:"attempt_at"`
}
