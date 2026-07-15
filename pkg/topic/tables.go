package topic

import "fmt"

// TODO - should reconsider if this should be *Topic methods

// LogTable is topicID's own physical message log.
func LogTable(topicID int64) string {
	return fmt.Sprintf("message_log_%d", topicID)
}

// PartitionTable is LogTable's nth partition -- message_log_<topic_id>_<n>.
func PartitionTable(topicID, n int64) string {
	return fmt.Sprintf("%s_%d", LogTable(topicID), n)
}

// DeliveryTable is topicID's own physical delivery table.
func DeliveryTable(topicID int64) string {
	return fmt.Sprintf("delivery_%d", topicID)
}
