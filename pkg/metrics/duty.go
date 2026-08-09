package metrics

import "time"

// DutySnapshot is one maintenance row's health.
type DutySnapshot struct {
	Duty          string
	TopicName     string
	ConsumerGroup string
	Rate          time.Duration
	GateAge       time.Duration // now() - can_run_after: negative while claimed into the future, positive once eligible and unclaimed
	Overdue       bool          // GateAge far past Rate -- nobody is maintaining this duty (or its owner is stuck)
	Attempts      int
}
