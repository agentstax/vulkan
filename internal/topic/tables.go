package topic

import "fmt"

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

// DeliveryLogTable is topicID's own physical delivery audit log -- absent
// when the topic was registered with DisableDeliveryLog.
func DeliveryLogTable(topicID int64) string {
	return fmt.Sprintf("delivery_log_%d", topicID)
}
