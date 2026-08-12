package cron

import "errors"

// GroupStatus is one consumer group's JobRequest outcomes for one cron job,
// derived from the job_requests delivery log.
type GroupStatus struct {
	ConsumerGroup string

	// Ran - job requests the group ran at least once.
	// Requests superseded or deferred are not counted.
	Ran int64

	// Succeeded - job requests with a 'success' delivery log row.
	Succeeded int64

	// Failed - job requests that raised without ever succeeding.
	Failed int64
}

func NewGroupStatus(consumerGroup string, ran int64, succeeded int64, failed int64) (*GroupStatus, error) {
	if consumerGroup == "" {
		return nil, errors.New("consumer group is required")
	}
	return &GroupStatus{
		ConsumerGroup: consumerGroup,
		Ran:           ran,
		Succeeded:     succeeded,
		Failed:        failed,
	}, nil
}
