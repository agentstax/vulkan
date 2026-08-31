package schedule

import (
	"encoding/json"
	"regexp"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
)

// a schedule's name doubles as the message key and routing key its messages are produced with,
// so it can't contain '*' -- the binding wildcard, which a pattern can't
// escape
var SlugPattern = regexp.MustCompile(`^[a-z0-9._-]+$`)

// ScheduleData is one row of schedule_config joined to its schedule_cursor row.
// Every schedule is the system's; TopicId is the target topic every produce
// lands on.
type ScheduleData struct {
	Id              int64                    `json:"schedule_id"`
	SystemId        int64                    `json:"system_id"`
	TopicId         int64                    `json:"topic_id"`
	Name            string                   `json:"schedule"`
	Expression      string                   `json:"expression"`
	SchemaVersion   int                      `json:"schema_version"`
	Concurrency     common.ConcurrencyPolicy `json:"concurrency"`
	Timeout         time.Duration            `json:"timeout"`
	Suspended       bool                     `json:"suspended"`
	Payload         json.RawMessage          `json:"payload"`
	Metadata        json.RawMessage          `json:"metadata"`
	NextScheduledAt time.Time                `json:"next_scheduled_at"`
	LastScheduledAt *time.Time               `json:"last_scheduled_at"`
}
