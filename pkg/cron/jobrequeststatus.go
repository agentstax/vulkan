package cron

import "time"

// JobRequestOutcome is where one job request ended up for one consumer group.
type JobRequestOutcome string

const (
	JobRequestSucceeded  JobRequestOutcome = "succeeded"  // ran to a 'success' delivery log row
	JobRequestFailed     JobRequestOutcome = "failed"     // raised without ever succeeding
	JobRequestSuperseded JobRequestOutcome = "superseded" // dropped unrun -- a newer request replaced it
	JobRequestDeferred   JobRequestOutcome = "deferred"   // waiting for a previous request to finish running
	JobRequestPending    JobRequestOutcome = "pending"    // produced, not yet run
)

// JobRequestStatus is one job request's outcome for one consumer group,
// newest request first in a CronJobRequests listing.
type JobRequestStatus struct {
	ConsumerGroup string            `json:"group"`
	MessageId     int64             `json:"message_id"`
	ScheduledTime time.Time         `json:"scheduled_time"`
	ProducedAt    time.Time         `json:"produced_at"`
	Outcome       JobRequestOutcome `json:"outcome"`

	// SupersededBy/SupersededAt - the replacing request's message id and
	// produce time, set only when Outcome is JobRequestSuperseded.
	SupersededBy *int64     `json:"superseded_by"`
	SupersededAt *time.Time `json:"superseded_at"`
}
