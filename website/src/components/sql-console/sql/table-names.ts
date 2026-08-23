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

export function deliveryTable(topicId: number): string {
	return `delivery_${topicId}`;
}

export function deliveryLogTable(topicId: number): string {
	return `delivery_log_${topicId}`;
}

export function cursorTable(topicId: number): string {
	return `cursor_${topicId}`;
}

export function leaseTable(topicId: number): string {
	return `lease_${topicId}`;
}

export function keyLeaseTable(topicId: number): string {
	return `key_lease_${topicId}`;
}

export function compactionHeadTable(topicId: number): string {
	return `compaction_head_${topicId}`;
}

export function bindingTable(topicId: number): string {
	return `binding_${topicId}`;
}

export function bindingLogTable(topicId: number): string {
	return `binding_log_${topicId}`;
}
