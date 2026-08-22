package topic

import (
	"fmt"
)

// MessageLogTable is topicID's own physical message log.
func MessageLogTable(topicID int64) string {
	return fmt.Sprintf("message_log_%d", topicID)
}

// MessageLogPartitionTable is MessageLogTable's nth partition -- message_log_<topic_id>_<n>.
func MessageLogPartitionTable(topicID, n int64) string {
	return fmt.Sprintf("%s_%d", MessageLogTable(topicID), n)
}

// DeliveryTable is topicID's own physical delivery table.
func DeliveryTable(topicID int64) string {
	return fmt.Sprintf("delivery_%d", topicID)
}

// DeliveryLogTable is topicID's own physical delivery audit log -- it exists
// for every topic; the topic's delivery_log_mode only gates the writes.
func DeliveryLogTable(topicID int64) string {
	return fmt.Sprintf("delivery_log_%d", topicID)
}

// IdempotencyKeyTable is topicID's own physical idempotency claim table.
func IdempotencyKeyTable(topicID int64) string {
	return fmt.Sprintf("idempotency_key_%d", topicID)
}

// CursorTable is topicID's own physical consumer-group cursor table.
func CursorTable(topicID int64) string {
	return fmt.Sprintf("cursor_%d", topicID)
}

// LeaseTable is topicID's own physical claimed-range lease table.
func LeaseTable(topicID int64) string {
	return fmt.Sprintf("lease_%d", topicID)
}

// KeyLeaseTable is topicID's own physical compaction-key lease table.
func KeyLeaseTable(topicID int64) string {
	return fmt.Sprintf("key_lease_%d", topicID)
}

// CompactionHeadTable is topicID's own physical compaction head index.
func CompactionHeadTable(topicID int64) string {
	return fmt.Sprintf("compaction_head_%d", topicID)
}

// BindingTable is topicID's own physical routing-rule table.
func BindingTable(topicID int64) string {
	return fmt.Sprintf("binding_%d", topicID)
}

// BindingLogTable is topicID's own physical binding declaration log.
func BindingLogTable(topicID int64) string {
	return fmt.Sprintf("binding_log_%d", topicID)
}
