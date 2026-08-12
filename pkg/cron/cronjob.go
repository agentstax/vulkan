package cron

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
)

// a job's name doubles as the routing key its job requests are produced with,
// so it can't contain '*' -- the binding wildcard, which Bind can't escape
var slugPattern = regexp.MustCompile(`^[a-z0-9._-]+$`)

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

func NewCronJob(
	id int64,
	systemId int64,
	topicId int64,
	consumerGroupId int64,
	name string,
	schedule string,
	concurrency common.ConcurrencyPolicy,
	timeout time.Duration,
	suspended bool,
	data json.RawMessage,
	metadata json.RawMessage,
	nextScheduledTime time.Time,
	lastScheduledTime *time.Time,
) (*CronJob, error) {
	if id <= 0 {
		return nil, fmt.Errorf("id must be > 0, got %d", id)
	}
	owners := 0
	for _, ownerId := range []int64{systemId, topicId, consumerGroupId} {
		if ownerId < 0 {
			return nil, fmt.Errorf("owner ids must be >= 0, got %d/%d/%d", systemId, topicId, consumerGroupId)
		}
		if ownerId > 0 {
			owners++
		}
	}
	if owners != 1 {
		return nil, fmt.Errorf("exactly one of systemId/topicId/consumerGroupId must be set, got %d/%d/%d", systemId, topicId, consumerGroupId)
	}
	if name == "" || schedule == "" {
		return nil, fmt.Errorf("name and schedule are required, got %q %q", name, schedule)
	}
	if err := concurrency.Validate(); err != nil {
		return nil, fmt.Errorf("concurrency: %w", err)
	}
	if timeout <= 0 {
		return nil, fmt.Errorf("timeout must be > 0, got %v", timeout)
	}
	if nextScheduledTime.IsZero() {
		return nil, errors.New("nextScheduledTime is required")
	}
	return &CronJob{
		Id:                id,
		SystemId:          systemId,
		TopicId:           topicId,
		ConsumerGroupId:   consumerGroupId,
		Name:              name,
		Schedule:          schedule,
		Concurrency:       concurrency,
		Timeout:           timeout,
		Suspended:         suspended,
		Data:              data,
		Metadata:          metadata,
		NextScheduledTime: nextScheduledTime,
		LastScheduledTime: lastScheduledTime,
	}, nil
}
