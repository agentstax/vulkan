// mirrors internal/topic's table-name functions -- the per-topic table name is
// the scope, so every family table interpolates the topic id
export function messageLogTable(topicId: number): string {
	return `message_log_${topicId}`;
}

export function messageLogPartitionTable(topicId: number, n: number): string {
	return `${messageLogTable(topicId)}_${n}`;
}

export function idempotencyKeyTable(topicId: number): string {
	return `idempotency_key_${topicId}`;
}

export function exceptionQueueTable(topicId: number): string {
	return `exception_queue_${topicId}`;
}

export function deliveryLogTable(topicId: number): string {
	return `delivery_log_${topicId}`;
}

export function consumerGroupCursorTable(topicId: number): string {
	return `consumer_group_cursor_${topicId}`;
}

export function claimLeaseTable(topicId: number): string {
	return `claim_lease_${topicId}`;
}

export function messageKeyLeaseTable(topicId: number): string {
	return `message_key_lease_${topicId}`;
}

export function compactionHeadTable(topicId: number): string {
	return `compaction_head_${topicId}`;
}

export function bindingConfigTable(topicId: number): string {
	return `binding_config_${topicId}`;
}

export function bindingConfigLogTable(topicId: number): string {
	return `binding_config_log_${topicId}`;
}
