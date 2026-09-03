// verbatim from pkg/produce/controller/datastore/insert.go protectedInsertSQL -- the
// template is drift-checked byte-exact; the function mirrors the fmt.Sprintf call
import { interpolate } from './interpolate';
import { idempotencyKeyTable, messageLogTable, compactionHeadTable } from './table-names';

export const protectedInsertCompactedSqlTemplate = `
			-- vulkan: produce.protectedInsert
			WITH claim AS (
				INSERT INTO %[1]s.%[2]s (idempotency_key)
				VALUES ($1)
				ON CONFLICT (idempotency_key) DO NOTHING
				RETURNING idempotency_key
			), inserted AS (
				INSERT INTO %[1]s.%[3]s (payload, routing_key, schema_version, message_key, compaction_rank, options)
				SELECT $2, NULLIF($3, ''), $4, $5, $6, $7  -- if routing_key $3 is empty string '' insert as NULL
				WHERE EXISTS (SELECT 1 FROM claim) -- if claim CTE didn't return anything skip this
				RETURNING id
			), latest AS (
				INSERT INTO %[1]s.%[4]s AS h (compaction_key, head_id, schema_version, compaction_rank)
				SELECT $5, id, $4, $6 FROM inserted
				ON CONFLICT (compaction_key) DO UPDATE
				SET head_id = EXCLUDED.head_id, schema_version = EXCLUDED.schema_version, compaction_rank = EXCLUDED.compaction_rank
				-- a newer payload version always wins; within a version rank first, then head_id
				WHERE (h.schema_version, h.compaction_rank, h.head_id) < (EXCLUDED.schema_version, EXCLUDED.compaction_rank, EXCLUDED.head_id)
			)
			SELECT id FROM inserted;
		`;

export function protectedInsertCompactedSql(topicId: number): string {
	return interpolate(
		protectedInsertCompactedSqlTemplate,
		idempotencyKeyTable(topicId),
		messageLogTable(topicId),
		compactionHeadTable(topicId),
	);
}
