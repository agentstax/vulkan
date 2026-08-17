package metrics

import "fmt"

// AbandonedRoutineKey is the routing key an abandoned/cleared GoRoutineEvent
// is produced under -- shared by the producing and reading sides so the two
// can never drift apart on the format.
func AbandonedRoutineKey(topicId int64, group string) string {
	return fmt.Sprintf("abandoned_routine.%d.%s", topicId, group)
}
