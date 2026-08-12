package cron

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
