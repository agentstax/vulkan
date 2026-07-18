package topic

import "fmt"

// MessageLogTable is topicID's own physical message log.
func MessageLogTable(topicID int64) string {
	return fmt.Sprintf("message_log_%d", topicID)
}

// PartitionTable is MessageLogTable's nth partition -- message_log_<topic_id>_<n>.
func PartitionTable(topicID, n int64) string {
	return fmt.Sprintf("%s_%d", MessageLogTable(topicID), n)
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
