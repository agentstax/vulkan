package metrics

import (
	"time"

	"github.com/agentstax/vulkan/pkg/common"
)

// ScheduleSnapshot is one schedule row's schedule health.
type ScheduleSnapshot struct {
	Owner      *common.Owner `json:"owner"` // the system -- every schedule is its
	Name       string        `json:"schedule"`
	Topic      string        `json:"topic"`
	Expression string        `json:"expression"`
	Suspended  bool          `json:"suspended"`

	NextScheduledAt time.Time     `json:"next_scheduled_at"`
	LastScheduledAt time.Time     `json:"last_scheduled_at"` // zero if the schedule has never been produced
	DueFor          time.Duration `json:"due_for"`           // now() - next_scheduled_at: negative until the slot arrives, positive while due and unproduced
	Overdue         bool          `json:"overdue"`           // due for longer than the overdue threshold and not suspended -- nothing is producing it
}
