package datastore

import "time"

type GroupData struct {
	Id        int64
	TopicId   int64
	Name      string
	CreatedAt time.Time
}
