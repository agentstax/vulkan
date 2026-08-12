package cron

import (
	"encoding/json"
	"regexp"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
)

// a job's name doubles as the routing key its job requests are produced with,
// so it can't contain '*' -- the binding wildcard, which Bind can't escape
var SlugPattern = regexp.MustCompile(`^[a-z0-9._-]+$`)

// CronJob is one row of cron_job.
type CronJob struct {
	Id                int64
	SystemId          int64
	TopicId           int64
	ConsumerGroupId   int64
	Name              string
	Schedule          string
	Concurrency       common.ConcurrencyPolicy
	Timeout           time.Duration
	Suspended         bool
	Data              json.RawMessage
	Metadata          json.RawMessage
	NextScheduledTime time.Time
	LastScheduledTime *time.Time
}
