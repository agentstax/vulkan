package datastore

import (
	"encoding/json"
	"time"
)

// DueCronJobData is the locked row snapshot one producing transaction works
// from.
type DueCronJobData struct {
	Id                int64           `db:"id"`
	Name              string          `db:"name"`
	Schedule          string          `db:"schedule"`
	Concurrency       string          `db:"concurrency"`
	Timeout           time.Duration   `db:"timeout_ns"`
	Data              json.RawMessage `db:"data"`
	Metadata          json.RawMessage `db:"metadata"`
	NextScheduledTime time.Time       `db:"next_scheduled_time"`
	DbNow             time.Time       `db:"now"`
}
