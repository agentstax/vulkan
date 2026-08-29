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

// ExceptionQueueTable is topicID's own physical exception queue -- deliveries
// off the mainline path: ready-to-retry, deferred, dead.
func ExceptionQueueTable(topicID int64) string {
	return fmt.Sprintf("exception_queue_%d", topicID)
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

// ConsumerGroupCursorTable is topicID's own physical consumer-group cursor table.
func ConsumerGroupCursorTable(topicID int64) string {
	return fmt.Sprintf("consumer_group_cursor_%d", topicID)
}

// ClaimLeaseTable is topicID's own physical claimed-range lease table.
func ClaimLeaseTable(topicID int64) string {
	return fmt.Sprintf("claim_lease_%d", topicID)
}

// KeyLeaseTable is topicID's own physical compaction-key lease table.
func KeyLeaseTable(topicID int64) string {
	return fmt.Sprintf("key_lease_%d", topicID)
}

// CompactionHeadTable is topicID's own physical compaction head index.
func CompactionHeadTable(topicID int64) string {
	return fmt.Sprintf("compaction_head_%d", topicID)
}

// BindingConfigTable is topicID's own physical routing-rule table.
func BindingConfigTable(topicID int64) string {
	return fmt.Sprintf("binding_config_%d", topicID)
}

// BindingConfigLogTable is topicID's own physical binding declaration log.
func BindingConfigLogTable(topicID int64) string {
	return fmt.Sprintf("binding_config_log_%d", topicID)
}
