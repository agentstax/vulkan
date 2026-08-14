package datastore

import "time"

type GroupData struct {
	Id        int64
	TopicId   int64
	Name      string
	CreatedAt time.Time
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
	Id              int64
	ConsumerGroupId int64
	GroupName       string
	TopicName       string
	SchemaVersion   int64
	Status          BindingDeclarationStatus
	Patterns        []string
	DeclaredBy      string
	DeclaredAt      time.Time
	AttemptAt       time.Time
}
