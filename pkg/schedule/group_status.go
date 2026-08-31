package schedule

// GroupStatus is one consumer group's outcomes for one schedule's messages,
// derived from the target topic's delivery log.
type GroupStatus struct {
	ConsumerGroup string `json:"group"`

	// Ran - messages the group ran at least once.
	// Messages superseded or deferred are not counted.
	Ran int64 `json:"ran_count"`

	// Succeeded - messages with a 'success' delivery log row.
	Succeeded int64 `json:"succeeded_count"`

	// Superseded - messages dropped unrun: a newer message replaced them
	// before this group ran them.
	Superseded int64 `json:"superseded_count"`

	// Failed - messages that raised without ever succeeding.
	Failed int64 `json:"failed_count"`
}
