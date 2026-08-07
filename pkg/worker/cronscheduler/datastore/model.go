package datastore

import (
	"encoding/json"
	"time"
)

// DueCronJobData is the locked row snapshot one producing transaction works
// from.
type DueCronJobData struct {
	Id                int64
	Name              string
	Schedule          string
	Concurrency       string
	Timeout           time.Duration
	Data              json.RawMessage
	Metadata          json.RawMessage
	NextScheduledTime time.Time
	DbNow             time.Time
}
