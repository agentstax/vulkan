package schedule

import (
	"encoding/json"
	"regexp"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
)

// a schedule's name doubles as the routing key its job requests are produced with,
// so it can't contain '*' -- the binding wildcard, which a pattern can't
// escape
var SlugPattern = regexp.MustCompile(`^[a-z0-9._-]+$`)

// Schedule is one row of schedule.
type Schedule struct {
	Id              int64                    `json:"schedule_id"`
	SystemId        int64                    `json:"system_id"`
	TopicId         int64                    `json:"topic_id"`
	ConsumerGroupId int64                    `json:"group_id"`
	Name            string                   `json:"schedule"`
	Expression      string                   `json:"expression"`
	Concurrency     common.ConcurrencyPolicy `json:"concurrency"`
	Timeout         time.Duration            `json:"timeout"`
	Suspended       bool                     `json:"suspended"`
	Payload         json.RawMessage          `json:"payload"`
	Metadata        json.RawMessage          `json:"metadata"`
	NextScheduledAt time.Time                `json:"next_scheduled_at"`
	LastScheduledAt *time.Time               `json:"last_scheduled_at"`
}
