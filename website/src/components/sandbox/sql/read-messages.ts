// verbatim from pkg/consumergroup/messageconsumer/controller/datastore/claim.go
// readMessages -- the template is drift-checked byte-exact; the function mirrors
// the fmt.Sprintf call
import { interpolate } from './interpolate';
import { bindingConfigTable, compactionHeadTable, messageLogTable } from './table-names';

export const readMessagesSqlTemplate = `
		-- vulkan: messageconsumer.readMessages
		SELECT
			m.id,
			m.payload,
			m.created_at,
			COALESCE(m.routing_key, '') AS routing_key,
			COALESCE(m.message_key, '') AS message_key,
			COALESCE(m.compaction_rank, 0) AS compaction_rank,
			(m.compaction_rank IS NOT NULL) AS compacted,
			m.options
		FROM %[1]s.%[2]s m
		WHERE m.id > $1
			AND m.id <= $2
			-- rows at another payload version pass under the cursor unread
			AND m.schema_version = $4
			AND (
				-- no bindings for consumer_group exists
				NOT EXISTS (
					SELECT 1 FROM %[1]s.%[3]s b
					WHERE b.consumer_group_id = $3
				)
				-- bindings for consumer_group exists and match routing_key pattern
				OR EXISTS (
					SELECT 1 FROM %[1]s.%[4]s b
					WHERE b.consumer_group_id = $3
						AND m.routing_key ~ b.pattern_regex
				)
				-- if bindings exist but our routing_key does not match any of them
				-- we do not return anything
			)
			AND (
				-- uncompacted rows (keyless or keyed) are never superseded
				m.compaction_rank IS NULL
				-- compacted rows are eligible only if they're compaction_head's
				-- current pointer for their key -- O(1) lookup, no per-row scan
				OR m.id = (
					SELECT head_id FROM %[1]s.%[5]s
					WHERE compaction_key = m.message_key
				)
			)
		-- rows MUST come back in id order or a batch LIMIT could
		-- return an arbitrary subset and the cursor would advance past unread offsets
		ORDER BY m.id;
	`;

export function readMessagesSql(topicId: number): string {
	return interpolate(
		readMessagesSqlTemplate,
		messageLogTable(topicId),
		bindingConfigTable(topicId),
		bindingConfigTable(topicId),
		compactionHeadTable(topicId),
	);
}
