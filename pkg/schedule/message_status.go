package schedule

import "time"

// MessageOutcome is where one of a schedule's messages ended up for one
// consumer group.
type MessageOutcome string

const (
	MessageSucceeded  MessageOutcome = "succeeded"  // ran to a 'success' delivery log row
	MessageFailed     MessageOutcome = "failed"     // raised without ever succeeding
	MessageSuperseded MessageOutcome = "superseded" // dropped unrun -- a newer message replaced it
	MessageDeferred   MessageOutcome = "deferred"   // waiting for a previous message to finish running
	MessagePending    MessageOutcome = "pending"    // produced, not yet run
)

// ScheduleMessageStatus is one of a schedule's messages' outcome for one consumer
// group, newest message first in a ScheduleMessages listing.
type ScheduleMessageStatus struct {
	ConsumerGroup string         `json:"group"`
	MessageId     int64          `json:"message_id"`
	ScheduledAt   time.Time      `json:"scheduled_at"`
	ProducedAt    time.Time      `json:"produced_at"`
	Outcome       MessageOutcome `json:"outcome"`

	// SupersededBy/SupersededAt - the replacing message's id and produce
	// time, set only when Outcome is MessageSuperseded.
	SupersededBy *int64     `json:"superseded_by"`
	SupersededAt *time.Time `json:"superseded_at"`
}
