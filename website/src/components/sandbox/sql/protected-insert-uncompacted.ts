// verbatim from pkg/produce/controller/datastore/insert.go protectedInsertSQL -- the
// template is drift-checked byte-exact; the function mirrors the fmt.Sprintf call
import { interpolate } from './interpolate';
import { idempotencyKeyTable, messageLogTable } from './table-names';

export const protectedInsertUncompactedSqlTemplate = `
			-- vulkan: produce.protectedInsert
			WITH claim AS (
				INSERT INTO %[1]s.%[2]s (idempotency_key)
				VALUES ($1)
				ON CONFLICT (idempotency_key) DO NOTHING
				RETURNING idempotency_key
			)
			INSERT INTO %[1]s.%[3]s (payload, routing_key, schema_version, message_key, options)
			SELECT
				$2,
				NULLIF($3, ''), -- if routing_key is empty string '' insert as NULL
				$4,
				NULLIF($5, ''), -- if message_key is empty string '' insert as NULL
				$6
			WHERE EXISTS (SELECT 1 FROM claim) -- if claim CTE didn't return anything skip this
			RETURNING id;
		`;

export function protectedInsertUncompactedSql(topicId: number): string {
	return interpolate(
		protectedInsertUncompactedSqlTemplate,
		idempotencyKeyTable(topicId),
		messageLogTable(topicId),
	);
}
