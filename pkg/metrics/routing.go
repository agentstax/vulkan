package metrics

import "fmt"

// AbandonedRoutineKey is the routing key an abandoned/cleared GoRoutineEvent
// is produced under -- shared by the producer (pkg/consumer/metrics) and the
// derived-metrics reader (pkg/metrics/datastore) so the two can never drift
// apart on the format.
func AbandonedRoutineKey(topicID int64, group string) string {
	return fmt.Sprintf("abandoned_routine.%d.%s", topicID, group)
}
