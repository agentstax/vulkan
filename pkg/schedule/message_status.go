package schedule

import "time"

// ScheduleMessageOutcome is where one of a schedule's messages ended up for one
// consumer group.
type ScheduleMessageOutcome string

const (
	ScheduleMessageSucceeded  ScheduleMessageOutcome = "succeeded"  // ran to a 'success' delivery log row
	ScheduleMessageFailed     ScheduleMessageOutcome = "failed"     // raised without ever succeeding
	ScheduleMessageSuperseded ScheduleMessageOutcome = "superseded" // dropped unrun -- a newer message replaced it
	ScheduleMessageDeferred   ScheduleMessageOutcome = "deferred"   // waiting for a previous message to finish running
	ScheduleMessagePending    ScheduleMessageOutcome = "pending"    // produced, not yet run
)

// ScheduleMessageStatus is one of a schedule's messages' outcome for one consumer
// group, newest message first in a ScheduleMessages listing.
type ScheduleMessageStatus struct {
	ConsumerGroup string                 `json:"group"`
	MessageId     int64                  `json:"message_id"`
	ScheduledAt   time.Time              `json:"scheduled_at"`
	ProducedAt    time.Time              `json:"produced_at"`
	Outcome       ScheduleMessageOutcome `json:"outcome"`

	// SupersededBy/SupersededAt - the replacing message's id and produce
	// time, set only when Outcome is ScheduleMessageSuperseded.
	SupersededBy *int64     `json:"superseded_by"`
	SupersededAt *time.Time `json:"superseded_at"`
}
