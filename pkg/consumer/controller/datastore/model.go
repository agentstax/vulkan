package datastore

import "time"

type GroupData struct {
	Id        int64     `db:"id"`
	TopicId   int64     `db:"topic_id"`
	Name      string    `db:"name"`
	CreatedAt time.Time `db:"created_at"`
}

// BindingDeclarationStatus is the binding_declaration.status column's value set.
type BindingDeclarationStatus string

const (
	BindingDeclarationInstalled BindingDeclarationStatus = "installed" // a set change that took effect
	BindingDeclarationWaiting   BindingDeclarationStatus = "waiting"   // an attempt blocked by a live instance's different set
)

// BindingDeclarationData is one binding_declaration row joined to the names
// a listing shows.
type BindingDeclarationData struct {
	Id              int64                    `db:"id"`
	ConsumerGroupId int64                    `db:"consumer_group_id"`
	GroupName       string                   `db:"group_name"`
	TopicName       string                   `db:"topic_name"`
	SchemaVersion   int64                    `db:"schema_version"`
	Status          BindingDeclarationStatus `db:"status"`
	Patterns        []string                 `db:"patterns"`
	DeclaredBy      string                   `db:"declared_by"`
	DeclaredAt      time.Time                `db:"declared_at"`
	AttemptAt       time.Time                `db:"attempt_at"`
}
