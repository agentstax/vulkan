package datastore

import (
	"encoding/json"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
)

// DueCronJobData is the locked row snapshot one producing transaction works
// from.
type DueCronJobData struct {
	Id              int64                    `db:"id"`
	Name            string                   `db:"name"`
	Schedule        string                   `db:"schedule"`
	Concurrency     common.ConcurrencyPolicy `db:"concurrency"`
	Timeout         time.Duration            `db:"timeout_ns"`
	Payload         json.RawMessage          `db:"payload"`
	Metadata        json.RawMessage          `db:"metadata"`
	NextScheduledAt time.Time                `db:"next_scheduled_at"`
	DbNow           time.Time                `db:"now"`
}
