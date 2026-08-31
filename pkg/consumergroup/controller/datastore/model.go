package datastore

import "time"

type ConsumerGroupConfigRow struct {
	Id        int64     `db:"id"`
	TopicId   int64     `db:"topic_id"`
	Name      string    `db:"name"`
	CreatedAt time.Time `db:"created_at"`
}

// BindingLogStatus is the binding_config_log.status column's value set.
type BindingLogStatus string

const (
	BindingLogInstalled BindingLogStatus = "installed" // a set change that took effect
	BindingLogWaiting   BindingLogStatus = "waiting"   // an attempt blocked by a live instance's different set
)

// BindingConfigLogRow is one binding_config_log row joined to the names
// a listing shows.
type BindingConfigLogRow struct {
	Id              int64            `db:"id"`
	ConsumerGroupId int64            `db:"consumer_group_id"`
	GroupName       string           `db:"group_name"`
	TopicName       string           `db:"topic_name"`
	Status          BindingLogStatus `db:"status"`
	Patterns        []string         `db:"patterns"`
	DeclaredBy      string           `db:"declared_by"`
	DeclaredAt      time.Time        `db:"declared_at"`
	AttemptedAt     time.Time        `db:"attempted_at"`
}
