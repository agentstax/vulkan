package datastore

import (
	"encoding/json"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
)

// DueScheduleData is the locked row snapshot one producing transaction works
// from.
type DueScheduleData struct {
	Id              int64                    `db:"id"`
	Name            string                   `db:"name"`
	Expression      string                   `db:"expression"`
	Concurrency     common.ConcurrencyPolicy `db:"concurrency"`
	Timeout         time.Duration            `db:"timeout_ns"`
	Payload         json.RawMessage          `db:"payload"`
	Metadata        json.RawMessage          `db:"metadata"`
	NextScheduledAt time.Time                `db:"next_scheduled_at"`
	DbNow           time.Time                `db:"now"`
}
