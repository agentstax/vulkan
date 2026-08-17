package metrics

import "fmt"

// AbandonedRoutineKey is the routing key an abandoned/cleared GoRoutineEvent
// is produced under -- shared by the producing and reading sides so the two
// can never drift apart on the format.
func AbandonedRoutineKey(topicId int64, group string) string {
	return fmt.Sprintf("abandoned_routine.%d.%s", topicId, group)
}

// EventType is the wire value an abandoned-routine event carries in its
// payload's type field -- shared by the producing and reading sides like the
// routing key above.
type EventType string

const (
	EventAbandoned EventType = "abandoned"
	EventCleared   EventType = "cleared"
)
