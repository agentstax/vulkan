package schedule

// GroupStatus is one consumer group's JobRequest outcomes for one schedule,
// derived from the job_requests delivery log.
type GroupStatus struct {
	ConsumerGroup string `json:"group"`

	// Ran - job requests the group ran at least once.
	// Requests superseded or deferred are not counted.
	Ran int64 `json:"ran_count"`

	// Succeeded - job requests with a 'success' delivery log row.
	Succeeded int64 `json:"succeeded_count"`

	// Superseded - job requests dropped unrun: a newer request replaced them
	// before this group ran them.
	Superseded int64 `json:"superseded_count"`

	// Failed - job requests that raised without ever succeeding.
	Failed int64 `json:"failed_count"`
}
