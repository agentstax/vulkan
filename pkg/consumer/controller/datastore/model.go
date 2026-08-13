package datastore

import "time"

type GroupData struct {
	Id        int64
	TopicId   int64
	Name      string
	CreatedAt time.Time
}

// BindingData is one binding row joined to the names a listing shows.
type BindingData struct {
	GroupName     string
	TopicName     string
	SchemaVersion int64
	Pattern       string // display when set, the stored regex otherwise
}
